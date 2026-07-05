package input

// package used for interactive user input for the CLI, wrapping up common needs/methods
// that prove useful to the CLI, so that various commands can use them accordingly

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/fatih/color"
)

// RequestInputType is an enum type representing a limited set of input request types, e.g. single value, selection etc.
type RequestInputType int

const (
	SingleValue RequestInputType = iota // 0
	Selection                           // 1
	YesNo                               // 2
)

// RequestInputOptions are options/args to pass to RequestInput
type RequestInputOptions struct {
	Type          RequestInputType
	Prompt        string
	SelectOptions []string
}

// RequestInputResult is an object representing the result of a RequestInput run
type RequestInputResult struct {
	StringValue string
	BoolValue   bool
}

// RequestInput is the main entrypoint for the user of this package, designating the type of prompt,
// returning the user input etc.
func RequestInput(opts *RequestInputOptions) (*RequestInputResult, error) {
	result := &RequestInputResult{}
	promptColor := color.RGB(255, 166, 0)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		_ = <-sigs
		os.Exit(0)
	}()

	switch opts.Type {
	case SingleValue:
		value, err := readLine(opts.Prompt)
		if err != nil {
			return result, err
		}
		result.StringValue = value
		return result, nil
	case Selection:
		menu := NewMenu(fmt.Sprintf("➡️ %s", promptColor.Sprint(opts.Prompt)))
		for _, selectOption := range opts.SelectOptions {
			menu.AddItem(selectOption, selectOption)
		}
		choice, err := menu.Display()
		if err == io.EOF {
			err = nil
		}
		result.StringValue = fmt.Sprintf("%v", choice)
		return result, err
	case YesNo:
		stdinReader := bufio.NewReader(os.Stdin)
		fmt.Printf("➡️ %s (y/n) ", promptColor.Sprint(opts.Prompt))
		yesNoResult, err := stdinReader.ReadString('\n')
		if err != nil && err != io.EOF {
			return result, err
		}
		yesNoResult = strings.TrimSpace(strings.ToLower(yesNoResult))
		if yesNoResult == "y" || yesNoResult == "yes" || yesNoResult == "1" {
			result.BoolValue = true
		}
		return result, nil
	}
	return result, nil
}
