package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const pobRepo = "PathOfBuildingCommunity/PathOfBuilding"

var httpClient = &http.Client{Timeout: 2 * time.Minute}

func get(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "poe-lib-gamedatagen")
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" && strings.HasPrefix(url, "https://api.github.com/") {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// timelessLUT are the timeless jewel lookup tables the engine reads (types
// 2-6; Glorious Vanity is split into large parts and not used yet; the
// abyss jewels' parts are selected by abyssLUT). They stay in the cache: see engine.TimelessDirEnv.
var timelessLUT = map[string]bool{
	"LethalPride.zip": true, "BrutalRestraint.zip": true, "MilitantFaith.zip": true,
	"ElegantHubris.zip": true, "HeroicTragedy.zip": true,
}

// wanted reports whether a repository path is needed for generation.
func wanted(p, tree string) bool {
	return (strings.HasPrefix(p, "src/Data/") && strings.HasSuffix(p, ".lua")) ||
		p == "src/TreeData/"+tree+"/tree.lua" || p == "src/Modules/ModParser.lua" || p == "src/Modules/Data.lua" || p == "src/Modules/ConfigOptions.lua" ||
		timelessLUT[strings.TrimPrefix(p, "src/Data/TimelessJewelData/")] || abyssLUT(p)
}

// abyssLUT selects the split lookup tables of the 3.30 abyssal timeless
// jewels (Data/TimelessJewelData/Abyss<Name>.zip.part<N>).
func abyssLUT(p string) bool {
	name, ok := strings.CutPrefix(p, "src/Data/TimelessJewelData/Abyss")
	return ok && strings.Contains(name, ".zip.part")
}

// fetchPoB downloads the needed files of a PoB tag into cacheDir (once) and
// returns the directory that plays the role of PoB's src.
func fetchPoB(tag, tree, cacheDir string) (string, error) {
	// fetchLayout changes whenever wanted() starts selecting more files.
	const fetchLayout = "6"
	root := filepath.Join(cacheDir, "pob-"+tag+"-"+fetchLayout)
	done := filepath.Join(root, ".complete")
	if _, err := os.Stat(done); err == nil {
		return filepath.Join(root, "src"), nil
	}
	body, err := get("https://api.github.com/repos/" + pobRepo + "/git/trees/" + tag + "?recursive=1")
	if err != nil {
		return "", err
	}
	var listing struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		return "", fmt.Errorf("tree listing: %w", err)
	}
	if listing.Truncated {
		return "", errors.New("tree listing truncated by GitHub; use -src with a local checkout")
	}
	var paths []string
	for _, e := range listing.Tree {
		if e.Type == "blob" && wanted(e.Path, tree) {
			paths = append(paths, e.Path)
		}
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no data files found for %s", tag)
	}

	jobs := make(chan string)
	errs := make(chan error, len(paths))
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				data, err := get("https://raw.githubusercontent.com/" + pobRepo + "/" + tag + "/" + p)
				if err == nil {
					dst := filepath.Join(root, filepath.FromSlash(p))
					if err = os.MkdirAll(filepath.Dir(dst), 0o755); err == nil {
						err = os.WriteFile(dst, data, 0o644)
					}
				}
				if err != nil {
					errs <- fmt.Errorf("%s: %w", p, err)
				}
			}
		}()
	}
	for _, p := range paths {
		jobs <- p
	}
	close(jobs)
	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		return "", err
	}
	if err := os.WriteFile(done, []byte(tag+"\n"), 0o644); err != nil {
		return "", err
	}
	return filepath.Join(root, "src"), nil
}
