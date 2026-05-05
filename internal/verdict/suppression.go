package verdict

import (
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

type Suppression struct {
	Critic   string `toml:"critic"`
	Title    string `toml:"title"`
	File     string `toml:"file,omitempty"`
	Reason   string `toml:"reason"`
	Expires  string `toml:"expires,omitempty"`
}

type SuppressionsFile struct {
	Suppressions []Suppression `toml:"suppression"`
}

func LoadSuppressions(path string) ([]Suppression, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var sf SuppressionsFile
	if err := toml.Unmarshal(data, &sf); err != nil {
		return nil, err
	}
	now := time.Now()
	var active []Suppression
	for _, s := range sf.Suppressions {
		if s.Expires != "" {
			t, err := time.Parse("2006-01-02", s.Expires)
			if err == nil && now.After(t) {
				continue
			}
		}
		active = append(active, s)
	}
	return active, nil
}

func FilterFindings(findings []Finding, suppressions []Suppression) []Finding {
	if len(suppressions) == 0 {
		return findings
	}
	var result []Finding
	for _, f := range findings {
		if isSuppressed(f, suppressions) {
			continue
		}
		result = append(result, f)
	}
	return result
}

func isSuppressed(f Finding, suppressions []Suppression) bool {
	for _, s := range suppressions {
		matched := true
		if s.Critic != "" && s.Critic != f.Critic {
			matched = false
		}
		if s.Title != "" && s.Title != f.Title {
			matched = false
		}
		if s.File != "" && s.File != f.File {
			matched = false
		}
		if matched && (s.Critic != "" || s.Title != "" || s.File != "") {
			return true
		}
	}
	return false
}

func SaveSuppressions(path string, suppressions []Suppression) error {
	sf := SuppressionsFile{Suppressions: suppressions}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(sf)
}