package gamedata

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

//go:embed poe1/*.json.gz poe1/version.json
var poe1FS embed.FS

// Data is the full data set for one game.
type Data struct {
	Version   Version
	Bases     map[string]*Base           // by base name
	Mods      map[string]map[string]*Mod // by domain ("Explicit", ...), then mod id
	Gems      map[string]*Gem            // by PoB gem id
	Uniques   []*Unique
	Tree      *Tree
	Cluster   *ClusterJewels
	Timeless  *Timeless
	Config    []ConfigOption // ConfigOptions.lua varList, in order
	Skills    *Skills
	Monster   *MonsterTables
	Pantheons map[string]*Pantheon // Data/Pantheons.lua, by config key ("TheBrineKing")
	Minions   map[string]*Minion   // data.minions, by minion id

	uniqueByName map[string]*Unique
}

// Unique returns the first unique with that name.
func (d *Data) Unique(name string) *Unique {
	return d.uniqueByName[name]
}

var (
	poe1Once sync.Once
	poe1     *Data
	poe1Err  error
)

// PoE1 loads the embedded Path of Exile 1 data once and returns it.
// The result is shared: callers must not modify it.
func PoE1() (*Data, error) {
	poe1Once.Do(func() { poe1, poe1Err = load(poe1FS, "poe1") })
	return poe1, poe1Err
}

func load(fsys embed.FS, dir string) (*Data, error) {
	d := &Data{}
	raw, err := fsys.ReadFile(dir + "/version.json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &d.Version); err != nil {
		return nil, fmt.Errorf("gamedata: version.json: %w", err)
	}
	files := map[string]any{
		"bases": &d.Bases, "mods": &d.Mods, "gems": &d.Gems, "uniques": &d.Uniques, "tree": &d.Tree,
		"clusterjewels": &d.Cluster, "timeless": &d.Timeless, "config": &d.Config, "skills": &d.Skills,
		"pantheons": &d.Pantheons, "monster": &d.Monster, "minions": &d.Minions,
	}
	// Each file decodes into its own field, so the files decode in
	// parallel; the first error in name order is returned.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	errs := make([]error, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() { errs[i] = decodeFile(fsys, dir, name, files[name]) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	d.uniqueByName = make(map[string]*Unique, len(d.Uniques))
	for _, u := range d.Uniques {
		if _, ok := d.uniqueByName[u.Name]; !ok {
			d.uniqueByName[u.Name] = u
		}
	}
	return d, nil
}

// decodeFile decodes dir/name.json.gz into dst.
func decodeFile(fsys embed.FS, dir, name string, dst any) error {
	gz, err := fsys.ReadFile(dir + "/" + name + ".json.gz")
	if err != nil {
		return err
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return fmt.Errorf("gamedata: %s: %w", name, err)
	}
	if derr := json.NewDecoder(zr).Decode(dst); derr != nil {
		return fmt.Errorf("gamedata: %s: %w", name, derr)
	}
	return nil
}
