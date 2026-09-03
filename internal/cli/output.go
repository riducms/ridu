package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

const cliLogTimeFormat = "15:04:05"

const serverBadgeColor = "86"

type cliOutputOptions struct {
	accessible    bool
	timeFunction  func(time.Time) time.Time
	stdoutProfile *colorprofile.Profile
	stderrProfile *colorprofile.Profile
}

// cliOutput owns the distinction between human-readable command results and
// operational diagnostics. Raw output remains byte-stable, while lifecycle
// events are serialized with managed child-process lines.
type cliOutput struct {
	stdout io.Writer
	stderr io.Writer

	writeMutex    sync.Mutex
	stdoutProfile colorprofile.Profile
	stderrProfile colorprofile.Profile
	timeFunction  func(time.Time) time.Time
}

func newCLIOutput(stdout, stderr io.Writer, options cliOutputOptions) *cliOutput {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}

	stdoutProfile := colorprofile.Detect(stdout, os.Environ())
	stderrProfile := colorprofile.Detect(stderr, os.Environ())
	if options.stdoutProfile != nil {
		stdoutProfile = *options.stdoutProfile
	}
	if options.stderrProfile != nil {
		stderrProfile = *options.stderrProfile
	}
	if options.accessible || environmentPresent("NO_COLOR") {
		stdoutProfile = colorprofile.NoTTY
		stderrProfile = colorprofile.NoTTY
	}

	output := &cliOutput{
		stdout:        stdout,
		stderr:        stderr,
		stdoutProfile: stdoutProfile,
		stderrProfile: stderrProfile,
		timeFunction:  options.timeFunction,
	}
	return output
}

func (output *cliOutput) rawWriters() (io.Writer, io.Writer) {
	return output.rawWriter(output.stdout), output.rawWriter(output.stderr)
}

func (output *cliOutput) rawWriter(writer io.Writer) io.Writer {
	if file, ok := writer.(terminalFile); ok {
		return cliTerminalRawWriter{
			cliRawWriter: cliRawWriter{output: output, writer: writer},
			file:         file,
		}
	}
	return cliRawWriter{output: output, writer: writer}
}

type cliOutputCarrier interface {
	cliOutput() *cliOutput
}

type cliRawWriter struct {
	output *cliOutput
	writer io.Writer
}

func (writer cliRawWriter) Write(content []byte) (int, error) {
	writer.output.writeMutex.Lock()
	defer writer.output.writeMutex.Unlock()
	return writer.writer.Write(content)
}

func (writer cliRawWriter) cliOutput() *cliOutput {
	return writer.output
}

func (writer cliRawWriter) unwrappedCLIWriter() io.Writer {
	return writer.writer
}

// terminalFile matches the file contract Bubble Tea uses to discover terminal
// dimensions and enter raw mode. Keeping this capability on serialized CLI
// writers prevents interactive forms from being initialized as a 0x0 screen.
type terminalFile interface {
	io.ReadWriteCloser
	Fd() uintptr
}

type cliTerminalRawWriter struct {
	cliRawWriter
	file terminalFile
}

func (writer cliTerminalRawWriter) Read(content []byte) (int, error) {
	return writer.file.Read(content)
}

func (writer cliTerminalRawWriter) Close() error {
	return writer.file.Close()
}

func (writer cliTerminalRawWriter) Fd() uintptr {
	return writer.file.Fd()
}

func environmentPresent(name string) bool {
	_, present := os.LookupEnv(name)
	return present
}

func (output *cliOutput) Info(message string, keyvals ...any) {
	output.writeLog(output.stdout, output.stdoutProfile, "", message+formatCLIFields(keyvals))
}

func (output *cliOutput) Warn(message string, err error) {
	output.writeLog(output.stderr, output.stderrProfile, "WARN", formatCLIDiagnostic(message, err))
}

func (output *cliOutput) Error(message string, err error) {
	output.writeLog(output.stderr, output.stderrProfile, "ERROR", formatCLIDiagnostic(message, err))
}

func (output *cliOutput) DevelopmentReady(version, duration, adminURL, apiURL string) {
	badge := lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("2")).Render(" ridu ")
	version = "v" + strings.TrimPrefix(strings.TrimSpace(version), "v")
	header := badge + " " + lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(version) + " " +
		lipgloss.NewStyle().Faint(true).Render("ready in") + " " + duration
	rail := lipgloss.NewStyle().Faint(true).Render("┃")
	urlStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	output.writeStyled(output.stdout, output.stdoutProfile, "\n"+header+"\n"+
		rail+" Admin  "+urlStyle.Render(adminURL)+"\n"+
		rail+" API    "+urlStyle.Render(apiURL)+"\n\n")
}

func (output *cliOutput) writeLog(writer io.Writer, profile colorprofile.Profile, level, message string) {
	now := time.Now()
	if output.timeFunction != nil {
		now = output.timeFunction(now)
	}
	timestamp := now.Format(cliLogTimeFormat)
	if level == "" {
		timestamp = lipgloss.NewStyle().Faint(true).Render(timestamp)
		scope := lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render("[ridu]")
		output.writeStyled(writer, profile, timestamp+" "+scope+" "+message+"\n")
		return
	}
	color := lipgloss.Color("3")
	if level == "ERROR" {
		color = lipgloss.Color("1")
	}
	timestampStyle := lipgloss.NewStyle().Foreground(color)
	if level == "ERROR" {
		timestampStyle = timestampStyle.Bold(true)
	}
	timestamp = timestampStyle.Render(timestamp)
	prefix := lipgloss.NewStyle().Foreground(color).Render("[" + level + "] [ridu]")
	output.writeStyled(writer, profile, timestamp+" "+prefix+" "+message+"\n")
}

func (output *cliOutput) writeStyled(writer io.Writer, profile colorprofile.Profile, content string) {
	var rendered bytes.Buffer
	profileWriter := colorprofile.Writer{Forward: &rendered, Profile: profile}
	_, _ = profileWriter.Write([]byte(content))
	output.writeMutex.Lock()
	defer output.writeMutex.Unlock()
	_, _ = writer.Write(rendered.Bytes())
}

func formatCLIFields(keyvals []any) string {
	var fields strings.Builder
	for index := 0; index+1 < len(keyvals); index += 2 {
		key := fmt.Sprint(keyvals[index])
		if key == "" {
			continue
		}
		value := fmt.Sprint(keyvals[index+1])
		if value == "" || strings.ContainsAny(value, " \t\r\n=\"") {
			value = strconv.Quote(value)
		}
		fmt.Fprintf(&fields, " %s=%s", key, value)
	}
	return fields.String()
}

func formatCLIDiagnostic(message string, err error) string {
	if err == nil {
		return message
	}
	encoded := err.Error()
	if !strings.Contains(encoded, "\n") {
		return message + ": " + encoded
	}
	return message + "\n  " + strings.ReplaceAll(formatStructuredCLIError(encoded), "\n", "\n  ")
}

// formatStructuredCLIError preserves the diagnostic text while giving each
// context segment in a multiline error chain its own physical line. This keeps
// validation failures readable in narrow terminals without guessing at child
// process severity or changing single-line error contracts.
func formatStructuredCLIError(encoded string) string {
	lines := strings.Split(encoded, "\n")
	if len(lines) < 2 {
		return encoded
	}
	context := strings.Split(lines[0], ": ")
	for index := range context {
		context[index] = embeddedLogTimestamp.ReplaceAllString(context[index], "")
	}
	formatted := append(context, lines[1:]...)
	last := len(formatted) - 1
	if index := strings.LastIndex(formatted[last], ": exit status "); index >= 0 {
		formatted = append(formatted, formatted[last][index+2:])
		formatted[last] = formatted[last][:index]
	}
	return strings.Join(formatted, "\n")
}

var embeddedLogTimestamp = regexp.MustCompile(`^(?:\d{4}/\d{2}/\d{2} )?\d{2}:\d{2}:\d{2} `)

func (output *cliOutput) sourceWriter(writer io.Writer, label string, profile colorprofile.Profile) *sourceWriter {
	if label == "admin" {
		return &sourceWriter{output: output, writer: writer}
	}
	badge := lipgloss.NewStyle().Foreground(lipgloss.Color(serverBadgeColor)).Render("[" + label + "]")
	var rendered bytes.Buffer
	profileWriter := colorprofile.Writer{Forward: &rendered, Profile: profile}
	_, _ = profileWriter.Write([]byte(badge))
	return &sourceWriter{
		output: output,
		writer: writer,
		prefix: append(rendered.Bytes(), ' '),
	}
}

// sourceWriter buffers a child process by physical line so its badge and
// content are committed atomically alongside Ridu-owned log records.
type sourceWriter struct {
	output  *cliOutput
	writer  io.Writer
	prefix  []byte
	mutex   sync.Mutex
	pending []byte
	closed  bool
}

func (writer *sourceWriter) Write(content []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	if writer.closed {
		return 0, os.ErrClosed
	}
	written := len(content)
	writer.pending = append(writer.pending, content...)
	for {
		newline := bytes.IndexByte(writer.pending, '\n')
		if newline < 0 {
			break
		}
		line := append([]byte(nil), writer.pending[:newline+1]...)
		writer.pending = writer.pending[newline+1:]
		if err := writer.writeLine(line); err != nil {
			return written, err
		}
	}
	return written, nil
}

func (writer *sourceWriter) Close() error {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	if writer.closed {
		return nil
	}
	writer.closed = true
	if len(writer.pending) == 0 {
		return nil
	}
	line := append([]byte(nil), writer.pending...)
	writer.pending = nil
	return writer.writeLine(line)
}

func (writer *sourceWriter) writeLine(line []byte) error {
	writer.output.writeMutex.Lock()
	defer writer.output.writeMutex.Unlock()
	if len(line) == 1 && line[0] == '\n' {
		_, err := writer.writer.Write(line)
		return err
	}
	if len(writer.prefix) != 0 {
		if _, err := writer.writer.Write(writer.prefix); err != nil {
			return err
		}
	}
	_, err := writer.writer.Write(line)
	return err
}

var _ io.WriteCloser = (*sourceWriter)(nil)
