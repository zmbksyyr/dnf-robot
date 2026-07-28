package marketapp

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	defaultServiceRunScript = "/root/run"
	defaultServiceHomeRoot  = "/home"
)

type marketServiceLaunch struct {
	dir    string
	bin    string
	args   []string
	source string
}

func (a *App) discoverMarketServiceLaunch(name, preferredRoot string) (marketServiceLaunch, error) {
	fallback := marketServiceLaunch{
		dir: filepath.Join(preferredRoot, name),
		bin: "./df_" + name + "_r",
	}
	runScript := strings.TrimSpace(a.serviceRunScript)
	if runScript == "" {
		runScript = defaultServiceRunScript
	}
	if launch, err := marketServiceLaunchFromRunScript(runScript, name); err == nil {
		return launch, nil
	}

	if launch, err := marketServiceLaunchFromDir(filepath.Join(preferredRoot, name), name, "service root"); err == nil {
		return launch, nil
	}

	homeRoot := strings.TrimSpace(a.serviceHomeRoot)
	if homeRoot == "" {
		homeRoot = defaultServiceHomeRoot
	}
	launches, err := marketServiceLaunchesUnderHome(homeRoot, name)
	if err != nil {
		return fallback, err
	}
	switch len(launches) {
	case 1:
		return launches[0], nil
	case 0:
		return fallback, fmt.Errorf("%s launch command not found in %s, %s, or %s", name, runScript, filepath.Join(preferredRoot, name), homeRoot)
	default:
		candidates := make([]string, 0, len(launches))
		for _, launch := range launches {
			candidates = append(candidates, filepath.Join(launch.dir, launch.bin)+" "+strings.Join(launch.args, " "))
		}
		return fallback, fmt.Errorf("%s launch command is ambiguous: %s", name, strings.Join(candidates, "; "))
	}
}

func marketServiceLaunchFromRunScript(path, name string) (marketServiceLaunch, error) {
	f, err := os.Open(path)
	if err != nil {
		return marketServiceLaunch{}, err
	}
	defer f.Close()

	wanted := "df_" + name + "_r"
	cwd := filepath.Dir(path)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		commands, err := splitShellCommands(scanner.Text())
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
				launch := marketServiceLaunch{dir: cwd, bin: field, args: append([]string(nil), fields[i+1:]...), source: path}
				if err := validateMarketServiceLaunch(launch); err == nil {
					return launch, nil
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return marketServiceLaunch{}, err
	}
	return marketServiceLaunch{}, fmt.Errorf("%s command not found in %s", wanted, path)
}

func marketServiceLaunchFromDir(dir, name, source string) (marketServiceLaunch, error) {
	binName := "df_" + name + "_r"
	bin := filepath.Join(dir, binName)
	if info, err := os.Stat(bin); err != nil || !info.Mode().IsRegular() {
		return marketServiceLaunch{}, fmt.Errorf("%s not found", bin)
	}
	configs, err := filepath.Glob(filepath.Join(dir, "cfg", name+"_*.cfg"))
	if err != nil {
		return marketServiceLaunch{}, err
	}
	sort.Strings(configs)
	if len(configs) != 1 {
		return marketServiceLaunch{}, fmt.Errorf("%s has %d matching configs", dir, len(configs))
	}
	configArg := "./" + filepath.ToSlash(mustRelativePath(dir, configs[0]))
	lastArg := binName
	if name == marketServiceNameAuction {
		lastArg = "./" + binName
	}
	return marketServiceLaunch{
		dir:    dir,
		bin:    "./" + binName,
		args:   []string{configArg, "start", lastArg},
		source: source,
	}, nil
}

func marketServiceLaunchesUnderHome(homeRoot, name string) ([]marketServiceLaunch, error) {
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
	launches := make([]marketServiceLaunch, 0, len(dirs))
	for _, dir := range dirs {
		launch, err := marketServiceLaunchFromDir(dir, name, "home scan")
		if err == nil {
			launches = append(launches, launch)
		}
	}
	return launches, nil
}

func validateMarketServiceLaunch(launch marketServiceLaunch) error {
	bin := launch.bin
	if !filepath.IsAbs(bin) {
		bin = filepath.Join(launch.dir, bin)
	}
	if info, err := os.Stat(bin); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("service binary not found: %s", bin)
	}
	for _, arg := range launch.args {
		if !strings.HasSuffix(strings.ToLower(arg), ".cfg") {
			continue
		}
		path := arg
		if !filepath.IsAbs(path) {
			path = filepath.Join(launch.dir, filepath.FromSlash(path))
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("service config not found: %s", path)
		}
		return nil
	}
	return fmt.Errorf("service config argument not found")
}

func splitShellCommands(line string) ([][]string, error) {
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
