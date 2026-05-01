package rules

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/NanoCycles/agent-brain/internal/domain"
	"gopkg.in/yaml.v3"
)

type Loader struct{}

func (Loader) Load(dir string) ([]domain.RuleSet, error) {
	var sets []domain.RuleSet
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !(strings.HasSuffix(d.Name(), ".yml") || strings.HasSuffix(d.Name(), ".yaml")) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var set domain.RuleSet
		if err := yaml.Unmarshal(data, &set); err != nil {
			return err
		}
		if set.Name == "" {
			set.Name = strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		}
		sets = append(sets, set)
		return nil
	})
	return sets, err
}

func Flatten(sets []domain.RuleSet) []domain.Rule {
	var out []domain.Rule
	for _, set := range sets {
		out = append(out, set.Rules...)
	}
	return out
}

func Match(all []domain.Rule, topics []string) []domain.Rule {
	seen := map[string]struct{}{}
	var matched []domain.Rule
	for _, r := range all {
		for _, topic := range topics {
			for _, rt := range r.Topics {
				if strings.Contains(strings.ToLower(topic), strings.ToLower(rt)) || strings.Contains(strings.ToLower(rt), strings.ToLower(topic)) {
					if _, ok := seen[r.ID]; !ok {
						matched = append(matched, r)
						seen[r.ID] = struct{}{}
					}
				}
			}
		}
	}
	return matched
}
