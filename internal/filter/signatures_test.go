package filter

import (
	"strings"
	"testing"
)

// extract runs aggressive extraction and returns the kept lines, trimmed.
func extract(t *testing.T, content string, lang Language) []string {
	t.Helper()
	_, lines, ok := ExtractSignaturesWithLineNums(content, lang)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, strings.TrimSpace(l))
	}
	return out
}

func has(got []string, want string) bool {
	for _, g := range got {
		if g == want {
			return true
		}
	}
	return false
}

// check asserts that every line in keep survives extraction and every line in
// drop does not.
func check(t *testing.T, name, content string, lang Language, keep, drop []string) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		got := extract(t, content, lang)
		for _, w := range keep {
			if !has(got, w) {
				t.Errorf("missing signature %q\ngot: %v", w, got)
			}
		}
		for _, d := range drop {
			if has(got, d) {
				t.Errorf("should not have extracted %q\ngot: %v", d, got)
			}
		}
	})
}

const goSrc = `package svc

import "fmt"

const MaxRetries = 3

var Default = &Client{}

type Client struct {
	url string
}

func New(url string) *Client {
	const localConst = "noise"
	var localVar int
	for i := range 3 {
		var deepNoise string
		_ = deepNoise
	}
	return &Client{url}
}

func (c *Client) Do() error { return nil }
`

func TestGoSignatures(t *testing.T) {
	check(t, "keeps file-scope declarations", goSrc, LangGo,
		[]string{
			"package svc",
			`import "fmt"`,
			"const MaxRetries = 3",
			"var Default = &Client{}",
			"type Client struct {",
			"func New(url string) *Client {",
			"func (c *Client) Do() error { return nil }",
		},
		// The regression this design exists to prevent: indented declarations
		// inside a function body are locals, not API.
		[]string{
			`const localConst = "noise"`,
			"var localVar int",
			"var deepNoise string",
			"url string",
		},
	)
}

const pySrc = `import os
from typing import List

CONFIG = {"a": 1}

@dataclass
class User:
    name: str

    @property
    def upper(self):
        return self.name.upper()

    async def fetch(self) -> dict:
        local = 1
        return {}

def _helper(a, b):
    return a + b
`

func TestPythonSignatures(t *testing.T) {
	check(t, "keeps defs, classes, decorators and imports", pySrc, LangPython,
		[]string{
			"import os",
			"from typing import List",
			"@dataclass",
			"class User:",
			"@property",
			"def upper(self):",
			"async def fetch(self) -> dict:",
			"def _helper(a, b):",
		},
		[]string{
			"local = 1",
			"return a + b",
			"name: str",
		},
	)
}

const rubySrc = `require 'json'

module Billing
  class Invoice < Base
    attr_reader :total
    belongs_to :user
    has_many :items
    scope :recent, -> { order(created_at: :desc) }
    validates :total, presence: true
    before_save :normalize

    def initialize(total)
      @total = total
    end

    def self.build(x)
      new(x)
    end

    private

    def secret
      42
    end
  end
end
`

func TestRubySignatures(t *testing.T) {
	check(t, "keeps declarative macros that form the public surface", rubySrc, LangRuby,
		[]string{
			"require 'json'",
			"module Billing",
			"class Invoice < Base",
			"attr_reader :total",
			"belongs_to :user",
			"has_many :items",
			"scope :recent, -> { order(created_at: :desc) }",
			"validates :total, presence: true",
			"before_save :normalize",
			"def initialize(total)",
			"def self.build(x)",
			"private",
			"def secret",
		},
		[]string{
			"@total = total",
			"new(x)",
			"42",
		},
	)
}

const tsSrc = `import { z } from "zod";

export const schema = z.object({});
const LOCAL = 5;
const handler = async (req: Request) => {};

export interface User { name: string }
export type ID = string;
export enum Kind { A, B }

export default class Service {
  private cache = new Map();
  constructor(private url: string) {}
  async fetch(id: ID): Promise<User> {
    const inner = 1;
    return null!;
  }
  get size() { return 1; }
}

export function helper(a: number) { return a; }
`

func TestTypeScriptSignatures(t *testing.T) {
	check(t, "keeps exports and members, drops plain locals", tsSrc, LangTypeScript,
		[]string{
			`import { z } from "zod";`,
			"export const schema = z.object({});",
			"export interface User { name: string }",
			"export type ID = string;",
			"export enum Kind { A, B }",
			"export default class Service {",
			"private cache = new Map();",
			"constructor(private url: string) {}",
			"async fetch(id: ID): Promise<User> {",
			"get size() { return 1; }",
			"export function helper(a: number) { return a; }",
		},
		[]string{
			"const LOCAL = 5;",
			"const inner = 1;",
			"return null!;",
		},
	)

	// A const that binds a function is a declaration; a const that binds a
	// value is a local. Both start with `const`, so only the former is kept.
	check(t, "keeps function-valued const", tsSrc, LangTypeScript,
		[]string{"const handler = async (req: Request) => {};"}, nil)
}

const jsSrc = `const express = require("express");
const PORT = 3000;

module.exports.handler = async (req, res) => {};
exports.other = function () {};

class Server {
  constructor(port) { this.port = port; }
  async start() {}
}

function helper(a) { return a; }
export const arrow = (x) => x;
`

func TestJavaScriptSignatures(t *testing.T) {
	check(t, "keeps CommonJS and ESM export forms", jsSrc, LangTypeScript,
		[]string{
			`const express = require("express");`,
			"module.exports.handler = async (req, res) => {};",
			"exports.other = function () {};",
			"class Server {",
			"constructor(port) { this.port = port; }",
			"async start() {}",
			"function helper(a) { return a; }",
			"export const arrow = (x) => x;",
		},
		[]string{"const PORT = 3000;"},
	)
}

func TestJavaScriptExtensionsMapToTypeScriptSpec(t *testing.T) {
	for _, name := range []string{"a.js", "a.jsx", "a.ts", "a.tsx"} {
		if got := DetectLanguage(name); got != LangTypeScript {
			t.Errorf("DetectLanguage(%q) = %v, want %v", name, got, LangTypeScript)
		}
	}
}

func TestLineNumbersMatchSource(t *testing.T) {
	nums, lines, ok := ExtractSignaturesWithLineNums(goSrc, LangGo)
	if !ok {
		t.Fatal("extraction failed")
	}
	src := strings.Split(goSrc, "\n")
	for i, n := range nums {
		if n < 1 || n > len(src) {
			t.Fatalf("line number %d out of range", n)
		}
		if src[n-1] != lines[i] {
			t.Errorf("line %d: reported %q but source has %q", n, lines[i], src[n-1])
		}
	}
}

func TestUnsupportedLanguageReportsFailure(t *testing.T) {
	if _, _, ok := ExtractSignaturesWithLineNums("#!/bin/sh\necho hi\n", LangUnknown); ok {
		t.Fatal("unknown language must report failure so the caller can choose another path")
	}
}

// A file whose every line is noise yields nothing, and that must be reported as
// a failure rather than as an empty successful extraction.
func TestNoMatchesReportsFailure(t *testing.T) {
	if _, _, ok := ExtractSignaturesWithLineNums("x := 1\ny := 2\n", LangGo); ok {
		t.Fatal("no matches must report failure")
	}
}

func TestEmptyAndBlankInput(t *testing.T) {
	for _, in := range []string{"", "\n", "   \n\t\n"} {
		if _, _, ok := ExtractSignaturesWithLineNums(in, LangGo); ok {
			t.Errorf("blank input %q must not report success", in)
		}
	}
}

// FilterContent is the other entry point and must agree with the line-numbered
// one about which lines are signatures.
func TestFilterContentAgreesWithLineNumberedExtraction(t *testing.T) {
	for _, tc := range []struct {
		lang Language
		src  string
	}{
		{LangGo, goSrc},
		{LangPython, pySrc},
		{LangRuby, rubySrc},
		{LangTypeScript, tsSrc},
	} {
		out, applied := FilterContent(tc.src, tc.lang, FilterAggressive)
		if !applied {
			t.Errorf("%v: FilterContent did not apply", tc.lang)
			continue
		}
		_, lines, ok := ExtractSignaturesWithLineNums(tc.src, tc.lang)
		if !ok {
			t.Errorf("%v: line-numbered extraction failed", tc.lang)
			continue
		}
		if got, want := strings.TrimRight(out, "\n"), strings.Join(lines, "\n"); got != want {
			t.Errorf("%v: entry points disagree\n got: %q\nwant: %q", tc.lang, got, want)
		}
	}
}

// Extraction must actually reduce, otherwise it is costing tokens for nothing.
func TestExtractionReducesSize(t *testing.T) {
	for _, tc := range []struct {
		lang Language
		src  string
	}{
		{LangGo, goSrc},
		{LangPython, pySrc},
		{LangRuby, rubySrc},
		{LangTypeScript, tsSrc},
		{LangTypeScript, jsSrc},
	} {
		out, _ := FilterContent(tc.src, tc.lang, FilterAggressive)
		if len(out) >= len(tc.src) {
			t.Errorf("%v: extraction did not reduce (%d -> %d)", tc.lang, len(tc.src), len(out))
		}
	}
}
