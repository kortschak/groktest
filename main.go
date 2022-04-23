// Copyright ©2022 Dan Kortschak. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The groktest command provides a command line interface for quickly testing
// Elasticsearch ingest pipeline grok processors.
package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/kortschak/ct"
	"golang.org/x/sys/execabs"
)

//go:embed base.patterns
var base []byte

func main() {
	grok := flag.String("grok", "", "path to a yaml grok processor (required) — may include line 'file.yml:<line>'")
	path := flag.String("path", "", "path to the grok input (required)")
	std := flag.String("base", "", "base pattern collection (optional)")
	verbose := flag.Bool("v", false, "run grok with debug=true")
	full := flag.Bool("full", false, "output complete JSON matching data")
	all := flag.Bool("all", false, "require that all lines match")
	flag.Parse()
	if *grok == "" || *path == "" {
		flag.Usage()
		os.Exit(2)
	}
	if *std != "" {
		var err error
		base, err = os.ReadFile(*std)
		if err != nil {
			log.Fatalf("failed to read base patterns: %v", err)
		}
	}

	cfg, err := grokConfig(*grok)
	if err != nil {
		log.Fatalf("failed to get grok config: %v", err)
	}
	cfg.Debug = *verbose
	cfg.Full = *full
	cfg.All = *all
	cfg.Input, err = filepath.Abs(*path)
	if err != nil {
		log.Fatal(err)
	}

	err = runGrok(cfg)
	if err != nil {
		log.Fatalf("grok failed: %v", err)
	}
}

func grokConfig(path string) (config, error) {
	path, line, useLine := strings.Cut(path, ":")
	b, err := os.ReadFile(path)
	if err != nil {
		return config{}, err
	}

	var n int
	if useLine {
		n, err = strconv.Atoi(line)
		if err != nil {
			return config{}, err
		}
	}

	file, err := parser.ParseBytes(b, 0)
	if err != nil {
		return config{}, err
	}
	if len(file.Docs) != 1 {
		return config{}, fmt.Errorf("unexpected number of yaml documents: %d", len(file.Docs))
	}
	v := &visitor{line: n, name: "grok"}
	ast.Walk(v, file.Docs[0])
	if v.node == nil {
		if v.line == 0 {
			return config{}, errors.New("no grok processor found")
		}
		return config{}, fmt.Errorf("no grok processor at line %d", n)
	}

	var cfg struct {
		Grok config `yaml:"grok"`
	}
	err = yaml.NodeToValue(v.node, &cfg)
	if len(cfg.Grok.Patterns) == 0 && err == nil {
		err = errors.New("no pattern")
	}
	return cfg.Grok, err
}

type visitor struct {
	line int
	name string
	node ast.Node
}

func (v *visitor) Visit(n ast.Node) ast.Visitor {
	tok := n.GetToken()
	if v.line != 0 && tok.Position.Line != v.line {
		return v
	}
	m, ok := n.(*ast.MappingValueNode)
	if !ok {
		return v
	}
	if m.Key.GetToken().Value == v.name {
		v.node = n
		return nil
	}
	return v
}

type config struct {
	Patterns    []string          `yaml:"patterns"`
	Definitions map[string]string `yaml:"pattern_definitions"`

	Input string
	Debug bool
	Full  bool

	All bool
}

var (
	capture  = regexp.MustCompile(`%{[a-zA-Z0-9_]+(?::[.a-zA-Z0-9_]+){1,2}}`)
	replacer = regexp.MustCompile(`[^a-zA-Z0-9_]`)
)

func runGrok(cfg config) error {
	work, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	buf := bytes.NewBuffer(base)
	buf.WriteByte('\n')
	for n, d := range cfg.Definitions {
		fmt.Fprintf(buf, "%s %s\n", n, d)
	}
	err = os.WriteFile(filepath.Join(work, "definitions"), buf.Bytes(), 0o644)
	if err != nil {
		return err
	}

	for i, p := range cfg.Patterns {
		cfg.Patterns[i] = capture.ReplaceAllStringFunc(p, func(m string) string {
			p, n, ok := strings.Cut(m, ":")
			if !ok || !strings.Contains(n, ".") {
				return m
			}
			return p + ":" + replacer.ReplaceAllString(n[:len(n)-1], "_") + "}"
		})
	}

	f, err := os.Create(filepath.Join(work, "program"))
	if err != nil {
		return err
	}
	err = prg.Execute(f, cfg)
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return err
	}

	cmd := execabs.Command("grok", "-f", "program")
	cmd.Dir = work
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if !cfg.All {
		return cmd.Run()
	}

	allMatched := true
	in, err := os.Open(cfg.Input)
	if err != nil {
		return err
	}
	defer in.Close()
	warn := (ct.Italic | ct.Fg(ct.BoldRed)).Paint
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		err = os.WriteFile(filepath.Join(work, "input"), append(sc.Bytes(), '\n'), 0o644)
		if err != nil {
			return err
		}
		cmd := execabs.Command("grok", "-f", "program")
		cmd.Dir = work
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		stdout := strings.TrimSpace(buf.String())
		switch {
		case stdout == "no match":
			fmt.Printf("%s: %s\n", stdout, warn(sc.Text()))
			allMatched = false
		case err != nil:
			return err
		default:
			fmt.Println(stdout)
		}
	}
	err = sc.Err()
	if err != nil {
		return err
	}
	if !allMatched {
		return errors.New("failed to match all inputs")
	}
	return nil
}

var prg = template.Must(template.New("grok").Parse(`program {
  load-patterns: "definitions"

  debug: {{.Debug}}

  exec "cat {{if .All}}input{{else}}{{.Input}}{{end}}"

  match {
    {{range .Patterns}}pattern: "{{.}}"
    {{end -}}
    reaction: "%{@JSON{{if .Full}}_COMPLEX{{end}}}"
  }

  {{- if .All}}no-match {
    {{range .Patterns}}pattern: "{{.}}"
    {{end -}}
    reaction: "no match"
  }{{- end -}}
}
`))
