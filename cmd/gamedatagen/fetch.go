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

// wanted reports whether a repository path is needed for generation.
func wanted(p, tree string) bool {
	return (strings.HasPrefix(p, "src/Data/") && strings.HasSuffix(p, ".lua")) ||
		p == "src/TreeData/"+tree+"/tree.lua"
}

// fetchPoB downloads the needed files of a PoB tag into cacheDir (once) and
// returns the directory that plays the role of PoB's src.
func fetchPoB(tag, tree, cacheDir string) (string, error) {
	root := filepath.Join(cacheDir, "pob-"+tag)
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
