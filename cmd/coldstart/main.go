// Command coldstart measures first-call latency in a fresh process: the
// steps a desktop tool takes right after it starts.
//
//	go run ./cmd/coldstart                       # default corpus build
//	go run ./cmd/coldstart -build 2HB8-0xwYEA6 -runs 11
//	go run ./cmd/coldstart -cpuprofile cpu.out -memprofile mem.out -phase calc1
//	go tool pprof -diff_base mem.out.base mem.out
//
// The game data and the parse cache are process-wide, so every run starts a
// new process: -runs re-executes this binary and prints the median of each
// step. -phase picks the step the profiles cover: new, calc1, calc2,
// comparer or item. Run it from the repository root (it reads
// testdata/golden/builds).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Mi7teR/exile-calc/engine"
	"github.com/Mi7teR/exile-calc/pobcode"
)

// ring is a rare ring as the game copies it (Ctrl+C).
const ring = "Item Class: Rings\r\nRarity: Rare\r\nGale Coil\r\nCoral Ring\r\n--------\r\nRequirements:\r\nLevel: 45\r\n--------\r\n" +
	"Item Level: 81\r\n--------\r\n+25 to maximum Life (implicit)\r\n--------\r\n+38 to maximum Life\r\n+40% to Fire Resistance\r\n" +
	"+12% to Cold Resistance (crafted)\r\n--------\r\nCorrupted\r\n"

// phaseNames are the measured steps, in order.
func phaseNames() []string { return []string{"new", "calc1", "calc2", "comparer", "item"} }

func main() {
	build := flag.String("build", "2HB8-0xwYEA6", "golden corpus build id (testdata/golden/builds)")
	runs := flag.Int("runs", 1, "fresh processes to run; the median of each step is printed")
	phase := flag.String("phase", "calc1", "step the profiles cover: "+strings.Join(phaseNames(), ", "))
	prof := profiles{}
	flag.StringVar(&prof.cpu, "cpuprofile", "", "write a CPU profile of -phase to this file")
	flag.StringVar(&prof.mem, "memprofile", "", "write an allocation profile of -phase to this file (and FILE.base)")
	flag.Parse()
	if prof.mem != "" {
		// Every allocation, so the -diff_base subtraction is exact.
		runtime.MemProfileRate = 1 //nolint:reassign // the documented way to set the sampling rate
	}
	if !slices.Contains(phaseNames(), *phase) {
		fail(fmt.Errorf("unknown phase %q", *phase))
	}
	if *runs > 1 {
		median(*runs)
		return
	}
	d, err := once(*build, *phase, prof)
	if err != nil {
		fail(err)
	}
	for i, p := range phaseNames() {
		fmt.Fprintf(os.Stdout, "%s %d\n", p, d[i].Microseconds())
	}
}

// median re-runs this binary n times with the same flags (minus -runs) and
// prints the median of each step.
func median(n int) {
	var args []string
	flag.Visit(func(f *flag.Flag) {
		if f.Name != "runs" {
			args = append(args, "-"+f.Name+"="+f.Value.String())
		}
	})
	phases := phaseNames()
	all := make([][]float64, len(phases))
	for range n {
		bin := os.Args[0]
		//nolint:gosec // this binary, its own flags
		self := exec.CommandContext(context.Background(), bin, args...)
		out, err := self.Output()
		if err != nil {
			fail(err)
		}
		for line := range strings.Lines(string(out)) {
			name, us, ok := strings.Cut(strings.TrimSpace(line), " ")
			i := slices.Index(phases, name)
			if !ok || i < 0 {
				continue
			}
			v, perr := strconv.ParseFloat(us, 64)
			if perr != nil {
				fail(perr)
			}
			all[i] = append(all[i], v)
		}
	}
	for i, p := range phases {
		slices.Sort(all[i])
		ms := all[i][len(all[i])/2] / float64(time.Millisecond/time.Microsecond)
		fmt.Fprintf(os.Stdout, "%-9s %8.1f ms (median of %d)\n", p, ms, len(all[i]))
	}
}

// once runs the steps in this process and returns how long each took.
func once(id, phase string, prof profiles) ([]time.Duration, error) {
	raw, err := os.ReadFile("testdata/golden/builds/" + id + ".code")
	if err != nil {
		return nil, err
	}
	b, err := pobcode.FromCode(string(raw))
	if err != nil {
		return nil, err
	}
	var (
		c  *engine.Calculator
		cp *engine.Comparer
	)
	steps := []func() error{
		func() error {
			var nerr error
			c, nerr = engine.New()
			return nerr
		},
		func() error { _, cerr := c.Calc(b); return cerr },
		func() error { _, cerr := c.Calc(b); return cerr },
		func() error {
			var nerr error
			cp, nerr = c.NewComparer(b)
			return nerr
		},
		func() error { _, ierr := cp.Item("Ring 1", ring); return ierr },
	}
	phases := phaseNames()
	d := make([]time.Duration, len(steps))
	for i, step := range steps {
		profiled := phases[i] == phase
		if profiled {
			if perr := prof.start(); perr != nil {
				return nil, perr
			}
		}
		start := time.Now()
		if serr := step(); serr != nil {
			return nil, fmt.Errorf("%s: %w", phases[i], serr)
		}
		d[i] = time.Since(start)
		if profiled {
			if perr := prof.stop(); perr != nil {
				return nil, perr
			}
		}
	}
	return d, nil
}

// profiles are the profile files to write; "" skips one.
type profiles struct{ cpu, mem string }

func (p profiles) start() error {
	if p.mem != "" {
		// The allocs profile is cumulative: the snapshot taken here is the
		// -diff_base for the one taken after the step.
		if err := writeAllocs(p.mem + ".base"); err != nil {
			return err
		}
	}
	if p.cpu == "" {
		return nil
	}
	f, err := os.Create(p.cpu)
	if err != nil {
		return err
	}
	return pprof.StartCPUProfile(f)
}

func (p profiles) stop() error {
	if p.cpu != "" {
		pprof.StopCPUProfile()
	}
	if p.mem == "" {
		return nil
	}
	return writeAllocs(p.mem)
}

// writeAllocs writes the allocs profile after a GC, which publishes the
// samples of the allocations since the last one.
func writeAllocs(name string) error {
	runtime.GC()
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	return errors.Join(pprof.Lookup("allocs").WriteTo(f, 0), f.Close())
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "coldstart:", err)
	os.Exit(1)
}
