// Command gamedatagen exports game data from Path of Building Community into
// gamedata/poe1. It reads PoB's Lua data files as data (see internal/luadata):
// no Lua runs and no PoB code is stored in this repository.
//
//	go run ./cmd/gamedatagen                 # download the pinned PoB tag
//	go run ./cmd/gamedatagen -src ~/PoB/src  # use a local checkout
//
// Re-run after bumping pobVersion / treeVersion for a new game patch.
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/internal/luadata"
)

// Pinned PoB release and passive tree.
const (
	pobVersion  = "v2.68.1"
	treeVersion = "3_30"
)

// modFiles are the Data/Mod*.lua files exported; they share one format.
var modFiles = []string{
	"ModCorrupted", "ModDelve", "ModEldritch", "ModExplicit", "ModFlask", "ModFoulborn",
	"ModGraft", "ModItemExclusive", "ModJewel", "ModJewelAbyss", "ModJewelCharm",
	"ModJewelCluster", "ModMaster", "ModMercenary", "ModNecropolis", "ModScourge",
	"ModSynthesis", "ModTincture", "ModVeiled",
}

func main() {
	src := flag.String("src", "", "PoB src directory (default: download "+pobVersion+")")
	out := flag.String("out", filepath.Join("gamedata", "poe1"), "game data output directory")
	parserOut := flag.String("modparser", "modparser", "modparser package directory (data/ and testdata/)")
	flag.Parse()
	if err := run(*src, *out, *parserOut); err != nil {
		slog.Error("gamedatagen failed", "err", err)
		os.Exit(1)
	}
}

func run(src, out, parserOut string) error {
	if src == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return err
		}
		slog.Info("fetching PoB", "version", pobVersion, "tree", treeVersion)
		if src, err = fetchPoB(pobVersion, treeVersion, filepath.Join(cache, "poe-lib")); err != nil {
			return err
		}
	}
	read := func(rel string) (string, error) {
		b, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(rel)))
		return string(b), err
	}
	glob := func(pattern string) (map[string]string, error) {
		matches, err := filepath.Glob(filepath.Join(src, filepath.FromSlash(pattern)))
		if err != nil {
			return nil, err
		}
		files := map[string]string{}
		for _, m := range matches {
			rel, _ := filepath.Rel(src, m)
			if files[filepath.ToSlash(rel)], err = read(filepath.ToSlash(rel)); err != nil {
				return nil, err
			}
		}
		return files, nil
	}

	baseFiles, err := glob("Data/Bases/*.lua")
	if err != nil {
		return err
	}
	bases, err := convertBases(baseFiles)
	if err != nil {
		return err
	}

	modSrc := map[string]string{}
	for _, f := range modFiles {
		rel := "Data/" + f + ".lua"
		if modSrc[rel], err = read(rel); err != nil {
			return err
		}
	}
	mods, err := convertMods(modSrc)
	if err != nil {
		return err
	}

	gemSrc, err := read("Data/Gems.lua")
	if err != nil {
		return err
	}
	gems, err := convertGems(gemSrc)
	if err != nil {
		return err
	}

	uniqueFiles, err := glob("Data/Uniques/*.lua") // Uniques/Special/* are not exported
	if err != nil {
		return err
	}
	uniques, err := convertUniques(uniqueFiles)
	if err != nil {
		return err
	}

	treeSrc, err := read("TreeData/" + treeVersion + "/tree.lua")
	if err != nil {
		return err
	}
	tree, err := convertTree(treeSrc)
	if err != nil {
		return err
	}

	clusterSrc, err := read("Data/ClusterJewels.lua")
	if err != nil {
		return err
	}
	var cluster gamedata.ClusterJewels
	if err := evalInto("Data/ClusterJewels.lua", clusterSrc, &cluster); err != nil {
		return err
	}

	timeless, err := convertTimeless(read)
	if err != nil {
		return err
	}

	configSrc, err := read("Modules/ConfigOptions.lua")
	if err != nil {
		return err
	}
	config, err := convertConfigOptions(configSrc)
	if err != nil {
		return err
	}

	mapModsSrc, err := read("Data/ModMap.lua")
	if err != nil {
		return err
	}
	mapMods, err := convertMapMods(mapModsSrc)
	if err != nil {
		return err
	}

	pantheonSrc, err := read("Data/Pantheons.lua")
	if err != nil {
		return err
	}
	var pantheons map[string]*gamedata.Pantheon
	if err := evalInto("Data/Pantheons.lua", pantheonSrc, &pantheons); err != nil {
		return err
	}

	miscSrc, err := read("Data/Misc.lua")
	if err != nil {
		return err
	}
	monster, err := convertMonsterTables(miscSrc)
	if err != nil {
		return err
	}

	globalSrc, err := read("Data/Global.lua")
	if err != nil {
		return err
	}
	skillEnv, err := parserEnv(globalSrc)
	if err != nil {
		return err
	}
	skills, err := convertSkills(read, skillEnv)
	if err != nil {
		return err
	}
	minions, err := convertMinions(read, skillEnv)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	version, _ := json.MarshalIndent(gamedata.Version{PoB: pobVersion, Tree: treeVersion}, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "version.json"), append(version, '\n'), 0o644); err != nil {
		return err
	}
	for name, v := range map[string]any{
		"bases": bases, "mods": mods, "gems": gems, "uniques": uniques, "tree": tree,
		"clusterjewels": &cluster, "timeless": timeless, "config": config, "mapmods": mapMods, "skills": skills, "pantheons": pantheons, "monster": monster, "minions": minions,
	} {
		if err := writeGzipJSON(filepath.Join(out, name+".json.gz"), v); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if err := writeModParser(read, tree, gems, parserOut); err != nil {
		return err
	}

	modCount := 0
	for _, m := range mods {
		modCount += len(m)
	}
	slog.Info("timeless jewel tables", "dir", filepath.Join(src, "Data", "TimelessJewelData"))
	slog.Info("generated", "out", out, "bases", len(bases), "mods", modCount, "modDomains", len(mods),
		"gems", len(gems), "skills", len(skills.Effects), "skippedSkillMods", skippedMods, "uniques", len(uniques), "treeNodes", len(tree.Nodes))
	return nil
}

// writeGzipJSON writes deterministic output: sorted map keys, no gzip timestamp.
func writeGzipJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := zw.Write(data); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func writeModParser(read func(string) (string, error), tree *gamedata.Tree, gems map[string]*gamedata.Gem, dir string) error {
	parserSrc, err := read("Modules/ModParser.lua")
	if err != nil {
		return err
	}
	globalSrc, err := read("Data/Global.lua")
	if err != nil {
		return err
	}
	clusterSrc, err := read("Data/ClusterJewels.lua")
	if err != nil {
		return err
	}
	dataSrc, err := read("Modules/Data.lua")
	if err != nil {
		return err
	}
	// data.keystones: the keystones ModParser turns into special mods.
	ksv, err := luadata.ExtractAssignment(dataSrc, "data.keystones", nil)
	if err != nil {
		return fmt.Errorf("Data.lua: %w", err)
	}
	var keystones []string
	if err := luadata.Decode(ksv, &keystones); err != nil {
		return fmt.Errorf("Data.lua keystones: %w", err)
	}
	tables, hooks, err := convertParserTables(parserSrc, globalSrc, clusterSrc, keystones)
	if err != nil {
		return err
	}
	// Modules/Data.lua tables the engine's ItemCondition tag reads.
	for _, name := range []string{"itemTagSpecial", "itemTagSpecialExclusionPattern"} {
		v, err := luadata.ExtractAssignment(dataSrc, "data."+name, nil)
		if err != nil {
			return fmt.Errorf("Data.lua: %w", err)
		}
		conv := &jsonConv{}
		tables[name] = conv.enc(v, name)
	}
	env, err := parserEnv(globalSrc)
	if err != nil {
		return err
	}
	skills, err := loadSkills(read, env)
	if err != nil {
		return err
	}
	skillType := env["SkillType"].(*luadata.Table)
	keywordFlag := env["KeywordFlag"].(*luadata.Table)
	buff, _ := skillType.Get("Buff").(luadata.Number)
	arrow, _ := keywordFlag.Get("Arrow").(luadata.Number)
	addSkillTables(tables, gems, skills, float64(buff), float64(arrow))
	addSkillLookups(tables, gems, skills)

	cacheSrc, err := read("Data/ModCache.lua")
	if err != nil {
		return err
	}
	cache, err := convertModCache(cacheSrc)
	if err != nil {
		return err
	}
	for _, d := range []string{filepath.Join(dir, "data"), filepath.Join(dir, "testdata")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	data := map[string]any{"pob": pobVersion, "tables": tables, "hooks": hooks}
	if err := writeGzipJSON(filepath.Join(dir, "data", "poe1.json.gz"), data); err != nil {
		return err
	}
	if err := writeGzipJSON(filepath.Join(dir, "testdata", "modcache.json.gz"), cache); err != nil {
		return err
	}
	slog.Info("modparser data", "tables", len(tables), "templates", templateCount, "hooks", len(hooks), "modCacheLines", len(cache))
	return nil
}
