// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"rsc.io/rf/refactor"
)

var showDiff = flag.Bool("diff", false, "show diff instead of writing files")

func usage() {
	fmt.Fprintf(os.Stderr, "usage: rf [-diff] script\n")
	os.Exit(2)
}

func main() {
	log.SetPrefix("rf: ")
	log.SetFlags(0)

	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		usage()
	}
	script := args[0]

	rf, err := refactor.New(".")
	if err != nil {
		log.Fatal(err)
	}
	rf.ShowDiff = *showDiff
	if err := run(rf, script); err != nil {
		log.Fatal(err)
	}
}

var cmds = map[string]func(*refactor.Snapshot, string){
	"add":    cmdAdd,
	"debug":  cmdDebug,
	"inline": cmdInline,
	"key":    cmdKey,
	"ex":     cmdEx,
	"mv":     cmdMv,
	"rm":     cmdRm,
}

type loader interface {
	Load() (*refactor.Snapshot, error)
}

func run(rf *refactor.Refactor, script string) error {
	var base loader = rf
	var snap *refactor.Snapshot

	text := script
	lastCmd := ""
	for text != "" {
		var line string
		line, text, _ = cut(text, "\n")
		line = trimComments(line)
		for strings.HasSuffix(line, `\`) && text != "" {
			var l string
			l, text, _ = cut(text, "\n")
			line = line[:len(line)-1] + "\n" + l
			line = trimComments(line)
		}
		line = strings.TrimLeft(line, " \t\n")
		if line == "" {
			continue
		}
		cmd, args, _ := cutAny(line, " \t")

		if rf.Debug["trace"] != "" {
			fmt.Fprintf(os.Stderr, "> %s\n", strings.ReplaceAll(line, "\n", "\\\n"))
		}

		fn := cmds[cmd]
		if fn == nil {
			return fmt.Errorf("unknown command %s", cmd)
		}

		var err error
		snap, err = base.Load()
		if err != nil {
			return err
		}
		if snap.Errors() > 0 {
			if lastCmd == "" {
				return fmt.Errorf("errors found before executing script")
			}
			base := base.(*refactor.Snapshot)
			if rf.ShowDiff {
				if d, err := base.Diff(); err == nil {
					rf.Stdout.Write(d)
				}
			} else {
				base.Write()
			}
			return fmt.Errorf("errors found after executing: %s", lastCmd)
		}
		x, _, ok := cut(line, "\n")
		if ok {
			x += " \\ ..."
		}
		lastCmd = x

		targ := snap.Target()
		if targ.Types == nil {
			println("TARG", targ, targ.PkgPath)
			panic("no types in target")
		}

		fn(snap, args)
		if snap.Errors() > 0 {
			return err
		}

		snap.Gofmt()
		base = snap
	}

	if snap == nil {
		// Did nothing.
		return nil
	}

	// Show diff before final load, so that it's easier to understand errors.
	if rf.ShowDiff {
		d, err := snap.Diff()
		if err != nil {
			return err
		}
		rf.Stdout.Write(d)
	}

	// Reload packages one last time before writing,
	// to make sure the rewrites are valid.
	if _, err := snap.Load(); err != nil {
		return fmt.Errorf("checking rewritten packages: %v", err)
	}

	if rf.ShowDiff {
		return nil
	}

	return snap.Write()
}

func trimComments(line string) string {
	// Cut line at # comment, being careful not to cut inside quoted text.
	var q byte
	for i := 0; i < len(line); i++ {
		switch c := line[i]; c {
		case q:
			q = 0
		case '\'', '"', '`':
			q = c
		case '\\':
			if q == '\'' || q == '"' {
				i++
			}
		case '#':
			if q == 0 {
				line = line[:i]
			}
		}
	}
	return strings.TrimSpace(line)
}

var isGoIdent = regexp.MustCompile(`^[\p{L}_][\p{L}\p{Nd}_]*$`)

func topItem(item *refactor.Item) *refactor.Item {
	for item != nil && item.Outer != nil {
		item = item.Outer
	}
	return item
}

func cut(s, sep string) (before, after string, ok bool) {
	if i := strings.Index(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

func cutAny(s, any string) (before, after string, ok bool) {
	if i := strings.IndexAny(s, any); i >= 0 {
		_, size := utf8.DecodeRuneInString(s[i:])
		return s[:i], s[i+size:], true
	}
	return s, "", false
}

func cutLast(s, sep string) (before, after string, ok bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

func cmdDebug(snap *refactor.Snapshot, text string) {
	for _, f := range strings.Fields(text) {
		key, val, ok := cut(f, "=")
		if !ok {
			val = "1"
		}
		snap.Refactor().Debug[key] = val
	}
}
