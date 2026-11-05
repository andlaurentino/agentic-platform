package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"

	"go.yaml.in/yaml/v3"
)

// Config is dev.yaml.
type Config struct {
	Services []Service `yaml:"services"`
}

// Service is one process devrun keeps running.
type Service struct {
	Name    string   `yaml:"name"`
	Dir     string   `yaml:"dir"`
	Prepare string   `yaml:"prepare"`
	Build   string   `yaml:"build"`
	Run     string   `yaml:"run"`
	Watch   []string `yaml:"watch"`
	Ext     []string `yaml:"ext"`
	Port    int      `yaml:"port"`
}

func loadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	var errs []error
	seen := map[string]bool{}
	for _, s := range c.Services {
		if s.Name == "" || s.Run == "" {
			errs = append(errs, fmt.Errorf("%s: every service needs a name and a run command", path))
		}
		if seen[s.Name] {
			errs = append(errs, fmt.Errorf("%s: duplicate service %q", path, s.Name))
		}
		seen[s.Name] = true
		if len(s.Watch) > 0 && len(s.Ext) == 0 {
			errs = append(errs, fmt.Errorf("%s: service %q watches directories but lists no ext", path, s.Name))
		}
	}
	return c, errors.Join(errs...)
}

// selectServices keeps the named services (all when names is empty).
func (c Config) selectServices(names []string) ([]Service, error) {
	if len(names) == 0 {
		return c.Services, nil
	}
	var out []Service
	for _, n := range names {
		i := slices.IndexFunc(c.Services, func(s Service) bool { return s.Name == n })
		if i < 0 {
			return nil, fmt.Errorf("unknown service %q", n)
		}
		out = append(out, c.Services[i])
	}
	return out, nil
}
