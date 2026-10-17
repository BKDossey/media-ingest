package core

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// NewFlags makes a FlagSet for a subcommand whose usage text is the given doc.
func NewFlags(name, doc string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() { fmt.Fprint(os.Stderr, doc) }
	return fs
}

// Parse parses args, allowing flags anywhere (before or after positional arguments),
// and returns the positional arguments. On error or -h it prints the doc and returns ok=false.
func Parse(fs *flag.FlagSet, args []string) (pos []string, ok bool) {
	var flags []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			flags = append(flags, a)
			name := strings.SplitN(strings.TrimLeft(a, "-"), "=", 2)[0]
			if f := fs.Lookup(name); f != nil && !strings.Contains(a, "=") && !isBool(f) && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	if err := fs.Parse(flags); err != nil {
		if err != flag.ErrHelp {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		fs.Usage()
		return nil, false
	}
	return pos, true
}

func isBool(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// Fail prints an error to stderr and returns exit code 1.
func Fail(format string, a ...any) int {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", a...)
	return 1
}
