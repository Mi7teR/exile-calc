package gamedata

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	dec := json.NewDecoder(zr)
	var derr error
	switch dst := dst.(type) {
	case *map[string]map[string]*Mod:
		// The largest file: its domains decode in parallel.
		*dst, derr = decodeParallel[map[string]*Mod](dec)
	case **Skills:
		// The second largest: the effects decode in parallel.
		*dst, derr = decodeSkills(dec)
	default:
		derr = dec.Decode(dst)
	}
	if derr != nil {
		return fmt.Errorf("gamedata: %s: %w", name, derr)
	}
	return nil
}

// decodeSkills decodes a Skills object with its effects split into chunks
// decoded in parallel. Every other field decodes through Skills itself
// (skillsFields), so a field added to Skills later is decoded too.
func decodeSkills(dec *json.Decoder) (*Skills, error) {
	var raw struct {
		skillsFields

		// Effects shadows skillsFields.Effects (the shallower field wins).
		Effects json.RawMessage `json:"effects"`
	}
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	s := (*Skills)(&raw.skillsFields)
	if raw.Effects == nil {
		return s, nil
	}
	var err error
	s.Effects, err = decodeParallel[*Skill](json.NewDecoder(bytes.NewReader(raw.Effects)))
	return s, err
}

// skillsFields is Skills without its methods, for decodeSkills.
type skillsFields Skills

// decodeParallel decodes a JSON object (or null) into a map, as Decode
// would, with the values decoded by several goroutines.
func decodeParallel[V any](dec *json.Decoder) (map[string]V, error) {
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil || raw == nil {
		return nil, err
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	const parts = 8
	out := make([]map[string]V, parts)
	errs := make([]error, parts)
	var wg sync.WaitGroup
	for p := range parts {
		wg.Go(func() {
			m := map[string]V{}
			for i := p; i < len(keys); i += parts {
				var v V
				if err := json.Unmarshal(raw[keys[i]], &v); err != nil {
					errs[p] = err
					return
				}
				m[keys[i]] = v
			}
			out[p] = m
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	all := make(map[string]V, len(raw))
	for _, m := range out {
		maps.Copy(all, m)
	}
	return all, nil
}
