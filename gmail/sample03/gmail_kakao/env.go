package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// loadEnv reads literal KEY=VALUE assignments; existing process variables win.
func loadEnv(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	keyPattern := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\uFEFF"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || !keyPattern.MatchString(key) {
			return fmt.Errorf("%s:%d: KEY=VALUE 形式で記述してください", path, lineNumber)
		}
		if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") {
			quote := value[0]
			end := strings.IndexByte(value[1:], quote)
			if end < 0 {
				return fmt.Errorf("%s:%d: 引用符が閉じていません", path, lineNumber)
			}
			end++
			tail := strings.TrimSpace(value[end+1:])
			if tail != "" && !strings.HasPrefix(tail, "#") {
				return fmt.Errorf("%s:%d: 引用符の後に不正な文字があります", path, lineNumber)
			}
			value = value[1:end]
		} else {
			for i := 1; i < len(value); i++ {
				if value[i] == '#' && (value[i-1] == ' ' || value[i-1] == '\t') {
					value = strings.TrimSpace(value[:i])
					break
				}
			}
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	for key, value := range values {
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("環境変数 %s を設定できません", key)
			}
		}
	}
	return nil
}
