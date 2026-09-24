package cli_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/avienor/bediz/internal/cli"
)

const agentSkillDir = "../../skills/bediz"

func TestAgentSkillDeclaresItsNameAndMatchingBedizVersion(t *testing.T) {
	frontmatter := agentSkillFrontmatter(t, readAgentSkill(t))

	if frontmatter["name"] != "bediz" {
		t.Fatalf("name = %q, want bediz", frontmatter["name"])
	}
	if strings.TrimSpace(frontmatter["description"]) == "" {
		t.Fatal("description is empty")
	}
	version := frontmatter["metadata.bediz-version"]
	if !regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`).MatchString(version) {
		t.Fatalf("metadata.bediz-version = %q, want a vX.Y.Z Bediz version", version)
	}
}

func TestAgentSkillShowsOnlyCommandsAndFlagsTheCLIAccepts(t *testing.T) {
	skill := readAgentSkillDocuments(t)
	index := newCLIHelpIndex(t)
	if len(index.commandSegments(skill)) == 0 {
		t.Fatal("the skill shows no bediz commands")
	}
	if problems := index.alignmentProblems(skill); len(problems) > 0 {
		t.Fatalf("the skill is not aligned with the CLI:\n%s", strings.Join(problems, "\n"))
	}
}

func TestAgentSkillAlignmentReportsUnknownCommandsAndFlags(t *testing.T) {
	skill := "Run `bediz queue purge --json`.\n\n" +
		"```sh\nbediz generate --style noir --json\nprintf '{}' | bediz frobnicate\n```\n\n" +
		"Approve `images wipe` and `queue clear --always`.\n"

	problems := newCLIHelpIndex(t).alignmentProblems(skill)

	for _, want := range []string{
		`"bediz queue purge --json": unknown command "bediz queue purge"`,
		`"bediz generate --style noir --json": "bediz generate" does not accept --style`,
		`"bediz frobnicate": unknown command "bediz frobnicate"`,
		`"images wipe": unknown command "bediz images wipe"`,
		`"queue clear --always": "bediz queue clear" does not accept --always`,
		`no bediz command accepts --always`,
		`no bediz command accepts --style`,
	} {
		if !slices.Contains(problems, want) {
			t.Errorf("problems do not contain %s:\n%s", want, strings.Join(problems, "\n"))
		}
	}
}

func readAgentSkill(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(agentSkillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

// readAgentSkillDocuments joins every Markdown file in the skill directory.
func readAgentSkillDocuments(t *testing.T) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(agentSkillDir, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	var documents []string
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		documents = append(documents, string(content))
	}
	return strings.Join(documents, "\n")
}

// agentSkillFrontmatter reads the flat and one-level nested scalar keys of the
// YAML frontmatter, keying nested values as "parent.child".
func agentSkillFrontmatter(t *testing.T, skill string) map[string]string {
	t.Helper()
	rest, ok := strings.CutPrefix(skill, "---\n")
	if !ok {
		t.Fatal("the skill does not start with YAML frontmatter")
	}
	block, _, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		t.Fatal("the skill frontmatter is not closed")
	}
	values := map[string]string{}
	parent := ""
	for line := range strings.SplitSeq(block, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if !strings.HasPrefix(line, " ") {
			parent = key
			if value != "" {
				values[key] = value
			}
			continue
		}
		values[parent+"."+key] = value
	}
	return values
}

var (
	fencedCodePattern = regexp.MustCompile("(?s)```[a-z]*\n(.*?)```")
	inlineCodePattern = regexp.MustCompile("`([^`\n]+)`")
	longFlagPattern   = regexp.MustCompile(`--[a-z0-9][a-z0-9-]*`)
	shellSeparator    = regexp.MustCompile(`\||&&|;`)
)

// skillCodeLines returns every line of fenced code and every inline code span.
func skillCodeLines(skill string) []string {
	var lines []string
	for _, block := range fencedCodePattern.FindAllStringSubmatch(skill, -1) {
		lines = append(lines, strings.Split(block[1], "\n")...)
	}
	prose := fencedCodePattern.ReplaceAllString(skill, "")
	for _, span := range inlineCodePattern.FindAllStringSubmatch(prose, -1) {
		lines = append(lines, span[1])
	}
	return lines
}

// skillCommandSegments returns each shell command segment that invokes bediz,
// and each code span that names a command path without the bediz prefix, such
// as "queue clear". A lone word such as "queue" may be a field name instead.
func skillCommandSegments(skill string, rootCommands []string) []string {
	var commands []string
	for _, line := range skillCodeLines(skill) {
		for _, segment := range shellSeparator.Split(line, -1) {
			fields := strings.Fields(segment)
			if len(fields) > 1 && (fields[0] == "bediz" || slices.Contains(rootCommands, fields[0])) {
				commands = append(commands, strings.TrimSpace(segment))
			}
		}
	}
	return commands
}

type cliHelp struct {
	subcommands []string
	flags       []string
}

type cliHelpIndex struct {
	t        *testing.T
	commands map[string]cliHelp
}

func newCLIHelpIndex(t *testing.T) *cliHelpIndex {
	t.Helper()
	index := &cliHelpIndex{t: t, commands: map[string]cliHelp{}}
	pending := [][]string{nil}
	for len(pending) > 0 {
		path := pending[0]
		pending = pending[1:]
		help := index.help(path)
		for _, subcommand := range help.subcommands {
			pending = append(pending, append(slices.Clone(path), subcommand))
		}
	}
	return index
}

func (index *cliHelpIndex) help(path []string) cliHelp {
	index.t.Helper()
	key := strings.Join(path, " ")
	if help, ok := index.commands[key]; ok {
		return help
	}
	var stdout, stderr bytes.Buffer
	exitCode := cli.New(&stdout, &stderr).Run(context.Background(), append(slices.Clone(path), "--help"))
	if exitCode != 0 {
		index.t.Fatalf("bediz %s --help exit code = %d, stderr = %q", key, exitCode, stderr.String())
	}
	var help cliHelp
	section := ""
	for line := range strings.SplitSeq(stdout.String(), "\n") {
		switch {
		case line == "":
			section = ""
		case !strings.HasPrefix(line, " "):
			section = line
		case section == "Available Commands:":
			help.subcommands = append(help.subcommands, strings.Fields(line)[0])
		case strings.HasSuffix(section, "Flags:"):
			help.flags = append(help.flags, longFlagPattern.FindString(line))
		}
	}
	index.commands[key] = help
	return help
}

func (index *cliHelpIndex) commandSegments(skill string) []string {
	return skillCommandSegments(skill, index.help(nil).subcommands)
}

func (index *cliHelpIndex) alignmentProblems(skill string) []string {
	var problems []string
	for _, command := range index.commandSegments(skill) {
		tokens := strings.Fields(command)
		if tokens[0] == "bediz" {
			tokens = tokens[1:]
		}
		var path []string
		for _, token := range tokens {
			if !slices.Contains(index.help(path).subcommands, token) {
				break
			}
			path = append(path, token)
		}
		help := index.help(path)
		if len(help.subcommands) > 0 {
			shown := append([]string{"bediz"}, tokens[:min(len(path)+1, len(tokens))]...)
			problems = append(problems, fmt.Sprintf("%q: unknown command %q", command, strings.Join(shown, " ")))
			continue
		}
		for _, flag := range longFlagPattern.FindAllString(command, -1) {
			if !slices.Contains(help.flags, flag) {
				problems = append(problems, fmt.Sprintf("%q: %q does not accept %s", command, strings.Join(append([]string{"bediz"}, path...), " "), flag))
			}
		}
	}

	var accepted []string
	for _, help := range index.commands {
		accepted = append(accepted, help.flags...)
	}
	for _, line := range skillCodeLines(skill) {
		for _, flag := range longFlagPattern.FindAllString(line, -1) {
			problem := "no bediz command accepts " + flag
			if !slices.Contains(accepted, flag) && !slices.Contains(problems, problem) {
				problems = append(problems, problem)
			}
		}
	}
	return problems
}
