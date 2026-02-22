package main

import (
	"flag"
	"fmt"
	"strconv"
)

// flagSet wraps flag.FlagSet to add support for uint64 flags.
type flagSet struct {
	*flag.FlagSet
}

// newCustomFlagSet creates a flagSet with ContinueOnError behavior.
func newCustomFlagSet(name string) *flagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return &flagSet{FlagSet: fs}
}

// Uint64Var defines a uint64 flag. Go's standard flag package lacks uint64
// support, so we use a custom Value implementation.
func (fs *flagSet) Uint64Var(p *uint64, name string, value uint64, usage string) {
	fs.FlagSet.Var(&uint64Value{p: p}, name, usage)
	*p = value
}

// Bool wraps flag.FlagSet.Bool.
func (fs *flagSet) Bool(name string, value bool, usage string) *bool {
	return fs.FlagSet.Bool(name, value, usage)
}

// uint64Value implements flag.Value for uint64 flags.
type uint64Value struct {
	p *uint64
}

func (v *uint64Value) String() string {
	if v.p == nil {
		return "0"
	}
	return strconv.FormatUint(*v.p, 10)
}

func (v *uint64Value) Set(s string) error {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid uint64 value %q", s)
	}
	*v.p = n
	return nil
}
