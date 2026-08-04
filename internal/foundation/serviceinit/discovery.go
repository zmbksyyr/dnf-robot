package serviceinit

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	DefaultRunScript = "/root/run"
	DefaultHomeRoot  = "/home"
)

// Launch describes one external DNF service command without executing it.
type Launch struct {
	Dir    string
	Bin    string
	Args   []string
	Source string
}

// DiscoverLaunch resolves a service command from the run script first, then
// the expected sibling directory, and finally an unambiguous scan under home.
func DiscoverLaunch(name, preferredRoot, runScript, homeRoot string) (Launch, error) {
	fallback := Launch{Dir: filepath.Join(preferredRoot, name), Bin: "./df_" + name + "_r"}
	if strings.TrimSpace(runScript) == "" {
		runScript = DefaultRunScript
	}
	if launch, err := LaunchFromRunScript(runScript, name); err == nil {
		return launch, nil
	}

	preferredDir := filepath.Join(preferredRoot, name)
	if launch, err := LaunchFromDir(preferredDir, name, "service root"); err == nil {
		return launch, nil
	}

	if strings.TrimSpace(homeRoot) == "" {
		homeRoot = DefaultHomeRoot
	}
	launches, err := LaunchesUnderHome(homeRoot, name)
	if err != nil {
		return fallback, err
	}
	switch len(launches) {
	case 1:
		return launches[0], nil
	case 0:
		return fallback, fmt.Errorf("%s launch command not found in %s, %s, or %s", name, runScript, preferredDir, homeRoot)
	default:
		candidates := make([]string, 0, len(launches))
		for _, launch := range launches {
			candidates = append(candidates, filepath.Join(launch.Dir, launch.Bin)+" "+strings.Join(launch.Args, " "))
		}
		return fallback, fmt.Errorf("%s launch command is ambiguous: %s", name, strings.Join(candidates, "; "))
	}
}

func LaunchFromRunScript(path, name string) (Launch, error) {
	f, err := os.Open(path)
	if err != nil {
		return Launch{}, err
	}
	defer f.Close()

	wanted := "df_" + name + "_r"
	cwd := filepath.Dir(path)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		commands, err := SplitShellCommands(scanner.Text())
		if err != nil {
			continue
		}
		for _, fields := range commands {
			if len(fields) == 0 {
				continue
			}
			if fields[0] == "cd" && len(fields) == 2 {
				cwd = resolveShellPath(cwd, fields[1])
				continue
			}
			for i, field := range fields {
				if filepath.Base(field) != wanted {
					continue
				}
				launch := Launch{Dir: cwd, Bin: field, Args: append([]string(nil), fields[i+1:]...), Source: path}
				if err := ValidateLaunch(launch); err == nil {
					return launch, nil
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Launch{}, err
	}
	return Launch{}, fmt.Errorf("%s command not found in %s", wanted, path)
}

func LaunchFromDir(dir, name, source string) (Launch, error) {
	binName := "df_" + name + "_r"
	bin := filepath.Join(dir, binName)
	if info, err := os.Stat(bin); err != nil || !info.Mode().IsRegular() {
		return Launch{}, fmt.Errorf("%s not found", bin)
	}
	configs, err := filepath.Glob(filepath.Join(dir, "cfg", name+"_*.cfg"))
	if err != nil {
		return Launch{}, err
	}
	sort.Strings(configs)
	if len(configs) != 1 {
		return Launch{}, fmt.Errorf("%s has %d matching configs", dir, len(configs))
	}
	configArg := "./" + filepath.ToSlash(mustRelativePath(dir, configs[0]))
	lastArg := binName
	if name == "auction" {
		lastArg = "./" + binName
	}
	return Launch{
		Dir:    dir,
		Bin:    "./" + binName,
		Args:   []string{configArg, "start", lastArg},
		Source: source,
	}, nil
}

func LaunchesUnderHome(homeRoot, name string) ([]Launch, error) {
	homeRoot = filepath.Clean(homeRoot)
	dirs := make([]string, 0)
	err := filepath.WalkDir(homeRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == homeRoot {
				return walkErr
			}
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(homeRoot, path)
		if err != nil {
			return err
		}
		depth := 0
		if rel != "." {
			depth = len(strings.Split(rel, string(filepath.Separator)))
		}
		if depth > 4 {
			return filepath.SkipDir
		}
		if entry.Name() == name {
			dirs = append(dirs, path)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	launches := make([]Launch, 0, len(dirs))
	for _, dir := range dirs {
		launch, err := LaunchFromDir(dir, name, "home scan")
		if err == nil {
			launches = append(launches, launch)
		}
	}
	return launches, nil
}

func ValidateLaunch(launch Launch) error {
	bin := launch.Bin
	if !filepath.IsAbs(bin) {
		bin = filepath.Join(launch.Dir, bin)
	}
	if info, err := os.Stat(bin); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("service binary not found: %s", bin)
	}
	if _, err := ConfigPath(launch); err != nil {
		return err
	}
	return nil
}

// ConfigPath returns the first existing config-like argument from a launch.
func ConfigPath(launch Launch) (string, error) {
	for _, arg := range launch.Args {
		ext := strings.ToLower(filepath.Ext(arg))
		if ext != ".cfg" && ext != ".ini" && ext != ".xml" {
			continue
		}
		path := arg
		if !filepath.IsAbs(path) {
			path = filepath.Join(launch.Dir, filepath.FromSlash(path))
		}
		path = filepath.Clean(path)
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("service config not found: %s", path)
		}
		return path, nil
	}
	return "", fmt.Errorf("service config argument not found")
}

// SplitShellCommands performs deliberately limited tokenization. It does not
// expand variables or execute shell syntax.
func SplitShellCommands(line string) ([][]string, error) {
	var commands [][]string
	var fields []string
	var word strings.Builder
	quote := byte(0)
	escaped := false
	flushWord := func() {
		if word.Len() > 0 {
			fields = append(fields, word.String())
			word.Reset()
		}
	}
	flushCommand := func() {
		flushWord()
		if len(fields) > 0 {
			commands = append(commands, fields)
			fields = nil
		}
	}
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if escaped {
			word.WriteByte(ch)
			escaped = false
			continue
		}
		if quote != '\'' && ch == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				word.WriteByte(ch)
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '#':
			if word.Len() == 0 {
				flushCommand()
				return commands, nil
			}
			word.WriteByte(ch)
		case ' ', '\t', '\r', '\n':
			flushWord()
		case ';', '|', '&':
			flushCommand()
		case '<', '>':
			flushCommand()
			return commands, nil
		default:
			word.WriteByte(ch)
		}
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("unterminated shell token")
	}
	flushCommand()
	return commands, nil
}

func resolveShellPath(cwd, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

func mustRelativePath(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return rel
}
