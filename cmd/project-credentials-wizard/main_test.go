package main

import (
	"bytes"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	projectsecrets "github.com/GuionAI/cloudnative-supabase/internal/resources/secrets"
)

type recordingClipboard struct {
	copied  []string
	current string
	clears  int
}

func (clipboard *recordingClipboard) Copy(value string) error {
	clipboard.copied = append(clipboard.copied, value)
	clipboard.current = value
	return nil
}

func (clipboard *recordingClipboard) Clear() error {
	clipboard.current = ""
	clipboard.clears++
	return nil
}

func TestWizardCopiesValidatedBundleWithoutRenderingCredentials(t *testing.T) {
	var output bytes.Buffer
	clipboard := &recordingClipboard{}
	input := strings.NewReader(strings.Repeat("\n", 7))

	if err := runWizard(input, &output, clipboard, time.Now(), rand.Reader); err != nil {
		t.Fatalf("runWizard() error = %v", err)
	}
	if len(clipboard.copied) != len(projectsecrets.RequiredProjectCredentialKeys) {
		t.Fatalf("copied field count = %d, want %d", len(clipboard.copied), len(projectsecrets.RequiredProjectCredentialKeys))
	}

	bundle := credentialBundle{
		SigningKeys:    clipboard.copied[0],
		PublishableKey: clipboard.copied[1],
		SecretKey:      clipboard.copied[2],
		AnonRoleJWT:    clipboard.copied[3],
		ServiceRoleJWT: clipboard.copied[4],
	}
	if _, err := projectsecrets.ValidateProjectCredentials(bundle.secret()); err != nil {
		t.Fatalf("copied bundle failed operator validation: %v", err)
	}
	for _, value := range clipboard.copied {
		if strings.Contains(output.String(), value) {
			t.Fatal("wizard rendered a credential value")
		}
	}
	if clipboard.current != "" || clipboard.clears < len(clipboard.copied)+1 {
		t.Fatal("wizard did not clear the clipboard")
	}
}

func TestWizardClearsClipboardWhenInputEnds(t *testing.T) {
	clipboard := &recordingClipboard{}
	err := runWizard(strings.NewReader("\n"), &bytes.Buffer{}, clipboard, time.Now(), rand.Reader)
	if err == nil {
		t.Fatal("runWizard() accepted incomplete input")
	}
	if len(clipboard.copied) != 1 {
		t.Fatalf("copied field count = %d, want 1", len(clipboard.copied))
	}
	if clipboard.current != "" {
		t.Fatal("wizard left a credential in the clipboard after failure")
	}
}

func TestOSC52ClipboardWritesOnlyToItsTerminal(t *testing.T) {
	var terminal bytes.Buffer
	clipboard := osc52Clipboard{terminal: &terminal}
	if err := clipboard.Copy("test"); err != nil {
		t.Fatalf("Copy() error = %v", err)
	}
	if got, want := terminal.String(), "\x1b]52;c;dGVzdA==\a"; got != want {
		t.Fatalf("Copy() output = %q, want %q", got, want)
	}
	if err := clipboard.Clear(); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if !strings.HasSuffix(terminal.String(), "\x1b]52;c;\a") {
		t.Fatal("Clear() did not emit an empty OSC52 payload")
	}
}
