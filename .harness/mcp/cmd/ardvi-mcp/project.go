package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func defaultHarnessDir() (string, error) {
	if value := os.Getenv("ARDVI_HARNESS_DIR"); value != "" {
		return filepath.Abs(value)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "ardvi", "harness"), nil
}

func runMake(dir string, environment []string, arguments ...string) error {
	command := exec.Command("make", append([]string{"--no-print-directory", "-C", dir}, arguments...)...)
	command.Env = append(os.Environ(), environment...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	return command.Run()
}

func projectInit(args []string) error {
	f := flag.NewFlagSet("init", flag.ContinueOnError)
	path := f.String("path", ".", "target Git repository")
	harness := f.String("harness", "", "installed harness bundle")
	prompt := f.String("prompt", "", "first task written to tasks/NEXT.md")
	promptFile := f.String("prompt-file", "", "file containing the first task")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *prompt != "" && *promptFile != "" {
		return errors.New("set only --prompt or --prompt-file")
	}
	var err error
	if *harness == "" {
		*harness, err = defaultHarnessDir()
		if err != nil {
			return err
		}
	}
	root, err := filepath.Abs(*path)
	if err != nil {
		return err
	}
	if _, err = os.Stat(filepath.Join(*harness, ".harness", "harness.mk")); err != nil {
		return fmt.Errorf("installed harness not found at %s; rerun the Ardvi release installer", *harness)
	}
	if _, err = os.Stat(filepath.Join(root, ".harness")); err == nil {
		return errors.New("project already contains .harness; run make init inside the project")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = runMake(*harness, nil, "copy", "TARGET="+root); err != nil {
		return err
	}
	environment := []string{"PROMPT=" + *prompt, "PROMPT_FILE=" + *promptFile}
	return runMake(root, environment, "harness-init")
}
