package provider

import (
	"fmt"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
)

const heapMiB = 1024 * 1024

type commandWord struct {
	value      string
	start, end int
}

// commandWords follows POSIX shlex quoting without evaluating shell expressions.
// Byte spans let memory edits preserve every unrelated argument verbatim.
func commandWords(command string) ([]commandWord, error) {
	if strings.ContainsRune(command, 0) {
		return nil, fmt.Errorf("launch command contains a NUL byte")
	}
	var words []commandWord
	for i := 0; i < len(command); {
		if strings.ContainsRune(" \t\r\n", rune(command[i])) {
			i++
			continue
		}
		start := i
		var value strings.Builder
		var quote byte
		for i < len(command) {
			c := command[i]
			if quote == 0 && strings.ContainsRune(" \t\r\n", rune(c)) {
				break
			}
			if c == quote {
				quote = 0
				i++
				continue
			}
			if quote == 0 && (c == '\'' || c == '"') {
				quote = c
				i++
				continue
			}
			if c == '\\' && quote != '\'' {
				if i+1 == len(command) {
					return nil, fmt.Errorf("launch command ends with an escape")
				}
				if quote == 0 || command[i+1] == '"' || command[i+1] == '\\' {
					i++
					c = command[i]
				}
			}
			value.WriteByte(c)
			i++
		}
		if quote != 0 {
			return nil, fmt.Errorf("launch command contains an unclosed quote")
		}
		words = append(words, commandWord{value: value.String(), start: start, end: i})
	}
	return words, nil
}

// javaOptions stops before application arguments, which may look like JVM flags.
func javaOptions(command string) ([]commandWord, int, error) {
	words, err := commandWords(command)
	if err != nil {
		return nil, 0, err
	}
	if len(words) == 0 || (path.Base(words[0].value) != "java" && path.Base(words[0].value) != "java.exe") {
		return nil, 0, fmt.Errorf("managed memory requires a direct Java command, not a script or shell wrapper")
	}
	for i := 1; i < len(words); i++ {
		arg := words[i].value
		if arg == "-jar" || arg == "-m" || arg == "--module" || strings.HasPrefix(arg, "--module=") || (!strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "@")) {
			return words, i, nil
		}
		switch arg {
		case "-cp", "-classpath", "--class-path", "-p", "--module-path", "--upgrade-module-path", "--add-modules", "--limit-modules", "--add-exports", "--add-opens", "--add-reads", "--patch-module", "--enable-native-access", "--source":
			i++
			if i == len(words) {
				return nil, 0, fmt.Errorf("java option %s is missing its argument", arg)
			}
		}
		for _, alternative := range []string{"-XX:InitialHeapSize=", "-XX:MaxHeapSize=", "-XX:InitialRAMPercentage=", "-XX:MaxRAMPercentage=", "-XX:MinRAMPercentage="} {
			if strings.HasPrefix(arg, alternative) {
				return nil, 0, fmt.Errorf("managed memory does not support alternative heap option %s; use -Xms and -Xmx", alternative)
			}
		}
	}
	return words, len(words), nil
}

func heapUnits(flag string) (float64, error) {
	value := flag[4:]
	multiplier := uint64(1)
	if len(value) > 0 {
		switch value[len(value)-1] {
		case 'k', 'K':
			multiplier = 1024
		case 'm', 'M':
			multiplier = heapMiB
		case 'g', 'G':
			multiplier = 1024 * heapMiB
		}
		if multiplier != 1 {
			value = value[:len(value)-1]
		}
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || n == 0 || n > math.MaxInt64/multiplier {
		return 0, fmt.Errorf("invalid Java heap option %s", flag[:4])
	}
	return float64(n*multiplier) / heapMiB / 1000, nil
}

func heapMemory(command string) (minimum, maximum *float64, err error) {
	words, end, err := javaOptions(command)
	if err != nil {
		return nil, nil, err
	}
	for _, word := range words[1:end] {
		if strings.HasPrefix(word.value, "-Xms") || strings.HasPrefix(word.value, "-Xmx") {
			value, err := heapUnits(word.value)
			if err != nil {
				return nil, nil, err
			}
			if strings.HasPrefix(word.value, "-Xms") {
				minimum = &value
			} else {
				maximum = &value
			}
		}
	}
	return minimum, maximum, nil
}

func memoryMiB(units float64) (int64, error) {
	mib := units * 1000
	if math.IsNaN(mib) || math.IsInf(mib, 0) || mib < 1000 || mib > float64(math.MaxInt64/heapMiB) || math.Round(mib)/1000 != units {
		return 0, fmt.Errorf("memory must be at least 1 unit and an exact whole number of JVM MiB (1 unit = 1000 MiB)")
	}
	return int64(math.Round(mib)), nil
}

func rewriteHeapMemory(command string, minimum, maximum float64) (string, error) {
	minMiB, err := memoryMiB(minimum)
	if err != nil {
		return "", err
	}
	maxMiB, err := memoryMiB(maximum)
	if err != nil {
		return "", err
	}
	words, end, err := javaOptions(command)
	if err != nil {
		return "", err
	}
	type edit struct {
		start, end int
		value      string
	}
	var edits []edit
	haveMin, haveMax, anchor := false, false, -1
	for _, word := range words[1:end] {
		isMin, isMax := strings.HasPrefix(word.value, "-Xms"), strings.HasPrefix(word.value, "-Xmx")
		if !isMin && !isMax {
			continue
		}
		if _, err := heapUnits(word.value); err != nil {
			return "", err
		}
		value := fmt.Sprintf("-Xmx%dM", maxMiB)
		if isMin {
			value = fmt.Sprintf("-Xms%dM", minMiB)
		}
		haveMin, haveMax = haveMin || isMin, haveMax || isMax
		anchor = word.start
		edits = append(edits, edit{word.start, word.end, value})
	}
	if !haveMin || !haveMax {
		if anchor == -1 {
			if end == len(words) {
				return "", fmt.Errorf("cannot locate JVM heap options or an explicit Java entry point; add inline -Xms and -Xmx before the application's argument file")
			}
			anchor = words[end].start
		}
		var insert string
		if !haveMin {
			insert += fmt.Sprintf("-Xms%dM ", minMiB)
		}
		if !haveMax {
			insert += fmt.Sprintf("-Xmx%dM ", maxMiB)
		}
		edits = append(edits, edit{anchor, anchor, insert})
	}
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start == edits[j].start {
			return edits[i].end < edits[j].end
		}
		return edits[i].start < edits[j].start
	})
	var result strings.Builder
	cursor := 0
	for _, change := range edits {
		result.WriteString(command[cursor:change.start])
		result.WriteString(change.value)
		cursor = change.end
	}
	result.WriteString(command[cursor:])
	return result.String(), nil
}
