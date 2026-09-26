// jenkins-delivery sends one trusted completed-build snapshot. Credentials are
// file references, never command arguments, environment values or output.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/laugh0608/RadishNexus/server/internal/jenkins"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("jenkins-delivery", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	config := flags.String("config", "", "absolute sender configuration path")
	input := flags.String("input", "", "absolute completed-build JSON path")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *config == "" || *input == "" {
		return errors.New("usage: jenkins-delivery -config /absolute/sender.json -input /absolute/completed-build.json")
	}
	raw, err := jenkins.ReadFile(*config, jenkins.MaxConfig)
	if err != nil {
		return err
	}
	sender, err := jenkins.LoadSender(raw, jenkins.ReadFile)
	if err != nil {
		return err
	}
	body, err := jenkins.ReadFile(*input, jenkins.MaxBody)
	if err != nil {
		return errors.New("cannot read completed-build input")
	}
	result, err := jenkins.Send(context.Background(), sender, body, nil)
	if err != nil {
		return err
	}
	if json.NewEncoder(out).Encode(result) != nil {
		return errors.New("cannot write delivery result")
	}
	return nil
}
