package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net"
	"net/url"
	"strings"

	"github.com/riducms/ridu"
)

// newSecret is prefix followed by 32 random bytes. The prefix makes a leaked
// token recognizable and tells the strategy which bearer tokens are its own.
func newSecret(prefix string) (string, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(random[:]), nil
}

// digest is what the plugin stores of a secret: its SHA-256, so a database
// read doesn't hand out working tokens.
func digest(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

const pkceAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

// validChallenge reports whether challenge is an S256 PKCE challenge: the
// unpadded base64url encoding of a SHA-256 hash.
func validChallenge(challenge string) bool {
	return len(challenge) == 43 && strings.Trim(challenge, pkceAlphabet) == ""
}

// verifierMatches reports whether a PKCE code verifier hashes to the S256
// challenge sent with the authorization request (RFC 7636).
func verifierMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 || strings.Trim(verifier, pkceAlphabet) != "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(expected), []byte(challenge)) == 1
}

// validRedirectURI reports whether a client may register uri: an https URL,
// or an http URL on a loopback address for a native client (RFC 8252),
// never with credentials or a fragment.
func validRedirectURI(uri string) bool {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Host == "" || parsed.Fragment != "" || parsed.User != nil || len(uri) > 2048 {
		return false
	}
	switch parsed.Scheme {
	case "https":
		return true
	case "http":
		return isLoopback(parsed.Hostname())
	}
	return false
}

// redirectMatches reports whether a requested redirect URI is one a client
// registered: the same string, or for a loopback address the same URL on any
// port, since native clients listen on a port chosen at run time (RFC 8252
// section 7.3).
func redirectMatches(registered, requested string) bool {
	if registered == requested {
		return true
	}
	want, err := url.Parse(registered)
	if err != nil {
		return false
	}
	got, err := url.Parse(requested)
	if err != nil || want.Scheme != "http" || got.Scheme != "http" || !isLoopback(want.Hostname()) || got.User != nil {
		return false
	}
	return strings.EqualFold(want.Hostname(), got.Hostname()) && want.Path == got.Path && want.RawQuery == got.RawQuery && got.Fragment == ""
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// sameResource compares resource indicators (RFC 8707), ignoring the case of
// the scheme and host and a trailing slash.
func sameResource(a, b string) bool {
	normalize := func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			return raw
		}
		return strings.ToLower(parsed.Scheme+"://"+parsed.Host) + strings.TrimSuffix(parsed.Path, "/")
	}
	return normalize(a) == normalize(b)
}

// oauthError answers a registration, token or revocation request in OAuth's
// error shape (RFC 6749 section 5.2), which clients read instead of Ridu's.
func oauthError(endpoint ridu.EndpointContext, status int, code, description string) {
	endpoint.JSON(status, map[string]string{"error": code, "error_description": description})
}
