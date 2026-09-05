/*
Copyright 2026 GuionAI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"
)

type clipboard interface {
	Copy(string) error
	Clear() error
}

type osc52Clipboard struct {
	terminal io.Writer
}

func (clipboard osc52Clipboard) Copy(value string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(value))
	_, err := fmt.Fprintf(clipboard.terminal, "\x1b]52;c;%s\a", encoded)
	return err
}

func (clipboard osc52Clipboard) Clear() error {
	_, err := io.WriteString(clipboard.terminal, "\x1b]52;c;\a")
	return err
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "project credential wizard: %v\n", err)
		os.Exit(1)
	}
}

func run() (err error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("run directly in an interactive terminal without redirected input or output")
	}

	terminal, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open controlling terminal: %w", err)
	}
	defer func() {
		if closeErr := terminal.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close controlling terminal: %w", closeErr)
		}
	}()

	clipboard := osc52Clipboard{terminal: terminal}
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupts)
	go func() {
		<-interrupts
		if err := clipboard.Clear(); err != nil {
			_, _ = fmt.Fprintf(
				os.Stderr,
				"\nwarning: clipboard may still contain credential material: %v\n",
				err,
			)
		}
		os.Exit(130)
	}()

	return runWizard(os.Stdin, os.Stdout, clipboard, time.Now(), rand.Reader)
}

func runWizard(
	input io.Reader,
	output io.Writer,
	clipboard clipboard,
	now time.Time,
	random io.Reader,
) (err error) {
	defer func() {
		if clearErr := clipboard.Clear(); clearErr != nil {
			err = errors.Join(err, fmt.Errorf("clear clipboard: %w", clearErr))
		}
	}()

	reader := bufio.NewReader(input)
	_, _ = fmt.Fprintln(output, "CloudNative Supabase project credential bundle")
	_, _ = fmt.Fprintln(output, "==============================================")
	_, _ = fmt.Fprintln(output)
	_, _ = fmt.Fprintln(output, "This generates one atomic five-field bundle in process memory.")
	_, _ = fmt.Fprintln(output, "Credential values are sent to the terminal clipboard without rendering plaintext.")
	_, _ = fmt.Fprintln(output)
	destinationPrompt := "Open the target project/environment/path in your secret manager, " +
		"then press Enter: "
	if err := waitForEnter(reader, output, destinationPrompt); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(output)
	_, _ = fmt.Fprintln(output, "Generating and validating the complete bundle...")
	bundle, err := generateCredentialBundle(now, random)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(output, "Bundle validated. Keep all five values from this run together.")

	fields := []struct {
		name  string
		value string
	}{
		{name: "signingKeys", value: bundle.SigningKeys},
		{name: "publishableKey", value: bundle.PublishableKey},
		{name: "secretKey", value: bundle.SecretKey},
		{name: "anonRoleJwt", value: bundle.AnonRoleJWT},
		{name: "serviceRoleJwt", value: bundle.ServiceRoleJWT},
	}
	for index, field := range fields {
		if err := clipboard.Copy(field.value); err != nil {
			return fmt.Errorf("copy %s: %w", field.name, err)
		}
		_, _ = fmt.Fprintf(output, "\n[%d/%d] %s is now in your clipboard.\n", index+1, len(fields), field.name)
		fieldPrompt := fmt.Sprintf(
			"Paste and save it under the exact key %q, then press Enter: ",
			field.name,
		)
		if err := waitForEnter(reader, output, fieldPrompt); err != nil {
			return err
		}
		if err := clipboard.Clear(); err != nil {
			return fmt.Errorf("clear clipboard after %s: %w", field.name, err)
		}
	}

	_, _ = fmt.Fprintln(output)
	_, _ = fmt.Fprintln(output, "All five fields were copied in sequence.")
	confirmationPrompt := "Confirm the destination contains all five exact keys, " +
		"then press Enter to finish: "
	if err := waitForEnter(reader, output, confirmationPrompt); err != nil {
		return err
	}
	if err := clipboard.Clear(); err != nil {
		return fmt.Errorf("clear clipboard: %w", err)
	}

	_, _ = fmt.Fprintln(output)
	_, _ = fmt.Fprintln(output, "Done. The clipboard is clear; the credential process will now exit.")
	return nil
}

func waitForEnter(reader *bufio.Reader, output io.Writer, prompt string) error {
	if _, err := io.WriteString(output, prompt); err != nil {
		return fmt.Errorf("write prompt: %w", err)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		return errors.New("input ended before the bundle was saved")
	}
	return nil
}
