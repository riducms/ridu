package migration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/riducms/ridu/schema"
)

// artifactWire is frozen independently from Artifact so later public API
// additions cannot silently change committed migration identity.
type artifactWire struct {
	Version                uint32          `json:"version"`
	Name                   string          `json:"name"`
	Planner                plannerWire     `json:"planner"`
	MinimumRunnerContract  uint32          `json:"minimumRunnerContract"`
	PreviousArtifactDigest string          `json:"previousArtifactDigest"`
	FromDigest             string          `json:"fromDigest"`
	ToDigest               string          `json:"toDigest"`
	Before                 json.RawMessage `json:"before,omitempty"`
	After                  json.RawMessage `json:"after"`
	Phases                 []phaseWire     `json:"phases"`
	Risks                  []riskWire      `json:"risks"`
}

type plannerWire struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type phaseWire struct {
	ID                      string     `json:"id"`
	Mode                    PhaseMode  `json:"mode"`
	PhysicalContractVersion uint32     `json:"physicalContractVersion"`
	BeforePhysicalDigest    string     `json:"beforePhysicalDigest"`
	AfterPhysicalDigest     string     `json:"afterPhysicalDigest"`
	Steps                   []stepWire `json:"steps"`
}

type stepWire struct {
	ID              string          `json:"id"`
	Kind            StepKind        `json:"kind"`
	ExecutorVersion uint32          `json:"executorVersion"`
	Name            string          `json:"name"`
	Payload         json.RawMessage `json:"payload"`
}

type riskWire struct {
	Code    string    `json:"code"`
	Level   RiskLevel `json:"level"`
	Message string    `json:"message"`
}

// NewArtifact creates an unpublished planner result with exact manifest
// lineage. Callers add ordered steps and risks before structural validation.
// The history publisher binds a non-initial result to its exact predecessor
// before it can be serialized or digested.
func NewArtifact(name string, planner Planner, before *schema.Manifest, after schema.Manifest) (Artifact, error) {
	toDigest, err := DigestManifest(after)
	if err != nil {
		return Artifact{}, err
	}
	artifact := Artifact{
		Version: ArtifactVersion, Name: name,
		Planner: planner, MinimumRunnerContract: RunnerContractVersion,
		ToDigest: toDigest, After: after.Snapshot(), Risks: []Risk{}, Phases: []Phase{},
	}
	if before != nil {
		fromDigest, err := DigestManifest(*before)
		if err != nil {
			return Artifact{}, err
		}
		snapshot := before.Snapshot()
		artifact.FromDigest, artifact.Before = fromDigest, &snapshot
	}
	return artifact, nil
}

// DecodeArtifact strictly decodes one frozen Ridu artifact format. Future
// versions and unknown fields are rejected before a runner can inspect a
// database.
func DecodeArtifact(encoded []byte) (Artifact, error) {
	if err := rejectDuplicateJSONKeys(encoded); err != nil {
		return Artifact{}, err
	}
	var header map[string]json.RawMessage
	if err := decodeStrictJSON(encoded, &header); err != nil {
		return Artifact{}, err
	}
	versionJSON, exists := header["version"]
	if !exists {
		return Artifact{}, fmt.Errorf("migration artifact version is required")
	}
	if _, exists := header["previousArtifactDigest"]; !exists {
		return Artifact{}, fmt.Errorf("migration artifact previous artifact digest is required")
	}
	var version uint32
	if err := json.Unmarshal(versionJSON, &version); err != nil {
		return Artifact{}, fmt.Errorf("decode migration artifact version: %w", err)
	}
	if version != ArtifactVersion {
		return Artifact{}, fmt.Errorf("unsupported migration artifact version %d", version)
	}
	var wire artifactWire
	if err := decodeStrictJSON(encoded, &wire); err != nil {
		return Artifact{}, err
	}
	artifact := artifactFromWire(wire)
	if err := artifact.decodeManifests(wire.Before, wire.After); err != nil {
		return Artifact{}, err
	}
	if err := artifact.Validate(); err != nil {
		return Artifact{}, err
	}
	if artifact.Before != nil && artifact.PreviousArtifactDigest == "" {
		return Artifact{}, fmt.Errorf("migration %s previous artifact digest is required", artifact.Name)
	}
	return artifact, nil
}

// MarshalJSON emits the frozen artifact wire contract.
func (artifact Artifact) MarshalJSON() ([]byte, error) {
	return artifact.canonicalBytes()
}

// UnmarshalJSON uses the same fail-closed versioned decoder as artifact files.
func (artifact *Artifact) UnmarshalJSON(encoded []byte) error {
	decoded, err := DecodeArtifact(encoded)
	if err != nil {
		return err
	}
	*artifact = decoded
	return nil
}

func (artifact Artifact) canonicalBytes() ([]byte, error) {
	if artifact.Before != nil && artifact.PreviousArtifactDigest == "" {
		return nil, fmt.Errorf("migration %s previous artifact digest is required", artifact.Name)
	}
	before, after, err := artifact.manifestEncodings()
	if err != nil {
		return nil, err
	}
	if artifact.Version != ArtifactVersion {
		return nil, fmt.Errorf("unsupported migration artifact version %d", artifact.Version)
	}
	return json.Marshal(artifactToWire(artifact, before, after))
}

func (artifact Artifact) manifestEncodings() (json.RawMessage, json.RawMessage, error) {
	var before json.RawMessage
	if artifact.Before != nil {
		if artifact.Before.Version != schema.CurrentVersion {
			return nil, nil, fmt.Errorf("migration %s before manifest uses unsupported schema version %d", artifact.Name, artifact.Before.Version)
		}
		encoded, err := json.Marshal(*artifact.Before)
		if err != nil {
			return nil, nil, err
		}
		before = encoded
	}
	if artifact.After.Version != schema.CurrentVersion {
		return nil, nil, fmt.Errorf("migration %s after manifest uses unsupported schema version %d", artifact.Name, artifact.After.Version)
	}
	after, err := json.Marshal(artifact.After)
	if err != nil {
		return nil, nil, err
	}
	return before, after, nil
}

// AfterManifest returns the validated immutable after snapshot.
func (artifact Artifact) AfterManifest() (schema.Manifest, error) {
	return validatedManifest(artifact.After)
}

// BeforeManifest returns the validated immutable before snapshot. Initial
// artifacts return an error because they intentionally have no parent state.
func (artifact Artifact) BeforeManifest() (schema.Manifest, error) {
	if artifact.Before == nil {
		return schema.Manifest{}, fmt.Errorf("before manifest is absent")
	}
	return validatedManifest(*artifact.Before)
}

// decodeManifests validates the embedded manifests of a decoded artifact.
func (artifact *Artifact) decodeManifests(before, after json.RawMessage) error {
	afterManifest, err := schema.ParseHistorical(after)
	if err != nil {
		return fmt.Errorf("migration %s after manifest: %w", artifact.Name, err)
	}
	artifact.After = afterManifest.Snapshot()
	if len(before) != 0 {
		beforeManifest, err := schema.ParseHistorical(before)
		if err != nil {
			return fmt.Errorf("migration %s before manifest: %w", artifact.Name, err)
		}
		snapshot := beforeManifest.Snapshot()
		artifact.Before = &snapshot
	}
	return validateArtifactManifestBoundary(*artifact)
}

func artifactFromWire(wire artifactWire) Artifact {
	artifact := Artifact{
		Version: wire.Version, Name: wire.Name,
		Planner: Planner{Name: wire.Planner.Name, Version: wire.Planner.Version}, MinimumRunnerContract: wire.MinimumRunnerContract,
		PreviousArtifactDigest: wire.PreviousArtifactDigest,
		FromDigest:             wire.FromDigest, ToDigest: wire.ToDigest,
		Phases: phasesFromWire(wire.Phases), Risks: risksFromWire(wire.Risks),
	}
	return artifact
}

func artifactToWire(artifact Artifact, before, after json.RawMessage) artifactWire {
	return artifactWire{
		Version: artifact.Version, Name: artifact.Name,
		Planner:                plannerWire{Name: artifact.Planner.Name, Version: artifact.Planner.Version},
		MinimumRunnerContract:  artifact.MinimumRunnerContract,
		PreviousArtifactDigest: artifact.PreviousArtifactDigest,
		FromDigest:             artifact.FromDigest, ToDigest: artifact.ToDigest, Before: before, After: after,
		Phases: phasesToWire(artifact.Phases), Risks: risksToWire(artifact.Risks),
	}
}

func phasesFromWire(wires []phaseWire) []Phase {
	phases := make([]Phase, len(wires))
	for phaseIndex, wire := range wires {
		phase := Phase{
			ID: wire.ID, Mode: wire.Mode, PhysicalContractVersion: wire.PhysicalContractVersion,
			BeforePhysicalDigest: wire.BeforePhysicalDigest, AfterPhysicalDigest: wire.AfterPhysicalDigest,
			Steps: make([]Step, len(wire.Steps)),
		}
		for stepIndex, step := range wire.Steps {
			phase.Steps[stepIndex] = Step{
				ID: step.ID, Kind: step.Kind, ExecutorVersion: step.ExecutorVersion,
				Name: step.Name, Payload: compactRaw(step.Payload),
			}
		}
		phases[phaseIndex] = phase
	}
	return phases
}

func phasesToWire(phases []Phase) []phaseWire {
	wires := make([]phaseWire, len(phases))
	for phaseIndex, phase := range phases {
		wire := phaseWire{
			ID: phase.ID, Mode: phase.Mode, PhysicalContractVersion: phase.PhysicalContractVersion,
			BeforePhysicalDigest: phase.BeforePhysicalDigest, AfterPhysicalDigest: phase.AfterPhysicalDigest,
			Steps: make([]stepWire, len(phase.Steps)),
		}
		for stepIndex, step := range phase.Steps {
			wire.Steps[stepIndex] = stepWire{
				ID: step.ID, Kind: step.Kind, ExecutorVersion: step.ExecutorVersion,
				Name: step.Name, Payload: compactRaw(step.Payload),
			}
		}
		wires[phaseIndex] = wire
	}
	return wires
}

func risksFromWire(wires []riskWire) []Risk {
	risks := make([]Risk, len(wires))
	for index, wire := range wires {
		risks[index] = Risk{Code: wire.Code, Level: wire.Level, Message: wire.Message}
	}
	return risks
}

func risksToWire(risks []Risk) []riskWire {
	wires := make([]riskWire, len(risks))
	for index, risk := range risks {
		wires[index] = riskWire{Code: risk.Code, Level: risk.Level, Message: risk.Message}
	}
	return wires
}

func compactRaw(encoded json.RawMessage) json.RawMessage {
	if len(encoded) == 0 {
		return nil
	}
	var compact bytes.Buffer
	if json.Compact(&compact, encoded) != nil {
		return append(json.RawMessage(nil), encoded...)
	}
	return append(json.RawMessage(nil), compact.Bytes()...)
}

func decodeStrictJSON(encoded []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func rejectDuplicateJSONKeys(encoded []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := scanJSONValue(decoder, "$", true); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder, path string, root bool) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key at %s is not a string", path)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON property %q at %s", key, path)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, path+"."+key, false); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("malformed JSON object at %s", path)
		}
	case '[':
		index := 0
		for decoder.More() {
			if err := scanJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index), false); err != nil {
				return err
			}
			index++
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("malformed JSON array at %s", path)
		}
	default:
		if root {
			return fmt.Errorf("migration artifact must be a JSON object")
		}
		return fmt.Errorf("unexpected JSON delimiter %q at %s", delimiter, path)
	}
	if root && delimiter != json.Delim('{') {
		return fmt.Errorf("migration artifact must be a JSON object")
	}
	return nil
}

func validateArtifactManifestBoundary(artifact Artifact) error {
	if artifact.After.Version != schema.CurrentVersion {
		return fmt.Errorf("migration artifact version %d cannot contain schema manifest version %d", artifact.Version, artifact.After.Version)
	}
	if artifact.Before != nil && artifact.Before.Version != schema.CurrentVersion {
		return fmt.Errorf("migration artifact version %d cannot contain schema manifest version %d", artifact.Version, artifact.Before.Version)
	}
	return nil
}
