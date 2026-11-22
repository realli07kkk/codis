// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

// Package cli provides the small command-line parsing layer shared by the
// Codis binaries. It builds on the standard library flag package and keeps
// the historical CLI contract:
//
//   - options may appear before or after positional arguments;
//   - long options accept both --flag=value and --flag value;
//   - short options accept -f value, -fvalue and clustering;
//   - boolean options never take a value;
//   - short spellings are declared explicitly through an alias table.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// String is a typed flag value that records whether the option was
// explicitly provided. The zero value means "not provided".
type String struct {
	Value    string
	Provided bool
}

// String implements flag.Value.
func (s *String) String() string { return s.Value }

// Set implements flag.Value. Empty values are rejected, matching the
// historical utils.Argument behavior of refusing an empty option argument.
func (s *String) Set(v string) error {
	if v == "" {
		return errors.New("requires a non-empty argument")
	}
	s.Value, s.Provided = v, true
	return nil
}

// Bool is a typed boolean flag value. Boolean options have no argument, so
// Provided is true exactly when the option appears on the command line.
type Bool struct {
	Value    bool
	Provided bool
}

// String implements flag.Value.
func (b *Bool) String() string { return strconv.FormatBool(b.Value) }

// Set implements flag.Value. Only the implicit "true" emitted by the flag
// package for a bare boolean option is accepted.
func (b *Bool) Set(v string) error {
	if v != "true" {
		return errors.New("must not have an argument")
	}
	b.Value, b.Provided = true, true
	return nil
}

// IsBoolFlag implements the interface the flag package uses to recognize
// boolean options that do not consume the following argument.
func (b *Bool) IsBoolFlag() bool { return true }

// Args is the result of a successful Parse.
type Args struct {
	Positionals []string
	Flags       map[string]bool
}

// IsSet reports whether the named option appeared on the command line.
func (a *Args) IsSet(name string) bool { return a.Flags[name] }

// CountSet returns how many of the named options appeared.
func (a *Args) CountSet(names ...string) int {
	n := 0
	for _, name := range names {
		if a.IsSet(name) {
			n++
		}
	}
	return n
}

// Only rejects options outside the allowed set.
func (a *Args) Only(allowed ...string) error {
	for name := range a.Flags {
		ok := false
		for _, candidate := range allowed {
			if name == candidate {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("unexpected option %s", FlagName(name))
		}
	}
	return nil
}

// ExactlyOne returns the single option set among names.
func (a *Args) ExactlyOne(names ...string) (string, error) {
	var found []string
	for _, name := range names {
		if a.IsSet(name) {
			found = append(found, name)
		}
	}
	if len(found) > 1 {
		return "", fmt.Errorf("at most one of %s is allowed", flagNames(names))
	}
	if len(found) == 0 {
		return "", fmt.Errorf("exactly one of %s is required", flagNames(names))
	}
	return found[0], nil
}

// AtMostOne rejects more than one of the named options.
func (a *Args) AtMostOne(names ...string) error {
	if a.CountSet(names...) > 1 {
		return fmt.Errorf("at most one of %s is allowed", flagNames(names))
	}
	return nil
}

// FlagName renders an internal option name with its documented spelling.
func FlagName(name string) string {
	if len(name) == 1 {
		return "-" + name
	}
	return "--" + name
}

func flagNames(names []string) string {
	labels := make([]string, len(names))
	for i, name := range names {
		labels[i] = FlagName(name)
	}
	return strings.Join(labels, ", ")
}

// Parse parses argv against fs. aliases maps short spellings (without a
// dash) onto their canonical long names; a self-mapping keeps a spelling
// short-only. Short values may be attached or separate and boolean short
// options may be clustered. It returns help=true for -h/--help without
// exiting.
func Parse(fs *flag.FlagSet, argv []string, aliases map[string]string) (*Args, bool, error) {
	fs.SetOutput(io.Discard)

	flags, positionals, err := normalize(fs, argv, aliases)
	if errors.Is(err, flag.ErrHelp) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := fs.Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, true, nil
		}
		return nil, false, err
	}
	args := &Args{Positionals: positionals, Flags: make(map[string]bool)}
	fs.Visit(func(f *flag.Flag) { args.Flags[f.Name] = true })
	return args, false, nil
}

// normalize rewrites argv into a flag package compatible form: all options
// first, then positional arguments. This preserves the documented flag forms
// (--flag=value, --flag value, -f value, -fvalue and short option clusters)
// and the historical behavior of accepting options anywhere on the command
// line.
func normalize(fs *flag.FlagSet, argv []string, aliases map[string]string) ([]string, []string, error) {
	var flags, positionals []string
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--" {
			// The docopt parser consumed the option terminator itself.
			positionals = append(positionals, argv[i+1:]...)
			break
		}
		if arg == "" || arg == "-" || !strings.HasPrefix(arg, "-") {
			positionals = append(positionals, arg)
			continue
		}

		if strings.HasPrefix(arg, "--") {
			body := arg[2:]
			name, value, hasValue := strings.Cut(body, "=")
			if name == "help" {
				return nil, nil, flag.ErrHelp
			}
			if _, ok := aliases[name]; ok {
				return nil, nil, fmt.Errorf("flag provided but not defined: --%s", name)
			}
			f := fs.Lookup(name)
			if f == nil {
				return nil, nil, fmt.Errorf("flag provided but not defined: %s", FlagName(name))
			}
			if isBoolFlag(f) {
				if hasValue {
					return nil, nil, fmt.Errorf("option %s must not have an argument", FlagName(name))
				}
				flags = append(flags, "--"+name)
				continue
			}
			if !hasValue {
				if i+1 >= len(argv) {
					return nil, nil, fmt.Errorf("flag needs an argument: %s", FlagName(name))
				}
				i++
				value = argv[i]
			}
			flags = append(flags, "--"+name+"="+value)
			continue
		}

		body := arg[1:]
		for j := 0; j < len(body); j++ {
			ch := body[j : j+1]
			if ch == "h" {
				return nil, nil, flag.ErrHelp
			}
			canonical, ok := aliases[ch]
			if !ok {
				return nil, nil, fmt.Errorf("flag provided but not defined: -%s", ch)
			}
			f := fs.Lookup(canonical)
			if f == nil {
				return nil, nil, fmt.Errorf("flag provided but not defined: %s", FlagName(canonical))
			}
			if isBoolFlag(f) {
				flags = append(flags, "--"+canonical)
				continue
			}
			value := body[j+1:]
			if value == "" {
				if i+1 >= len(argv) {
					return nil, nil, fmt.Errorf("flag needs an argument: %s", FlagName(canonical))
				}
				i++
				value = argv[i]
			}
			flags = append(flags, "--"+canonical+"="+value)
			break
		}
	}
	// Keep a terminator in the normalized flag package input even though it is
	// not exposed as a positional argument to callers.
	flags = append(flags, "--")
	return append(flags, positionals...), positionals, nil
}

func isBoolFlag(f *flag.Flag) bool {
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}
