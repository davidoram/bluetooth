package hps

import (
	"fmt"
	"strings"
)

// Define a new type that can accept multiple values passed on the command line
type ArrayStr []string

func (i *ArrayStr) String() string {
	return strings.Join([]string(*i), "\n")
}

func (i *ArrayStr) Set(value string) error {
	if len(strings.Split(value, "=")) != 2 {
		return fmt.Errorf("Invalid format, expect 'key=value'")
	}
	*i = append(*i, value)
	return nil
}
