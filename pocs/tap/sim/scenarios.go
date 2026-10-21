package sim

import (
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"slices"
	"sync"
	"time"
)

// Scenario is one end-to-end check against a running server.
type Scenario struct {
	Name string
	Run  func(base string) (detail string, err error)
}

var phoneBase, _ = rand.Int(rand.Reader, big.NewInt(9_000_000))
var phoneMu sync.Mutex
var phoneN int64

// nextPhone returns a fresh E.164 number so repeated runs against one
// server never collide.
func nextPhone() string {
	phoneMu.Lock()
	defer phoneMu.Unlock()
	phoneN++
	return fmt.Sprintf("+1%010d", 2_000_000_000+phoneBase.Int64()*100+phoneN)
}

func onboardAll(base string, names ...string) ([]*Phone, error) {
	var out []*Phone
	for _, n := range names {
		p := NewPhone(base, n)
		if err := p.Onboard(nextPhone()); err != nil {
			return nil, fmt.Errorf("onboard %s: %w", n, err)
		}
		out = append(out, p)
	}
	return out, nil
}

func matched(r Result) error {
	if r.Err != nil {
		return r.Err
	}
	if r.Match.Status != "matched" {
		return fmt.Errorf("status %q", r.Match.Status)
	}
	return nil
}

// Scenarios covers the happy path, the attacks the design names, event
// mode and a venue-peak burst.
func Scenarios(burstPairs int) []Scenario {
	return []Scenario{
		{"happy path", func(base string) (string, error) {
			ps, err := onboardAll(base, "Alice", "Bob")
			if err != nil {
				return "", err
			}
			start := time.Now()
			ra, rb, err := Handshake(ps[0], ps[1], DefaultMeasurement(), DefaultMeasurement())
			if err != nil {
				return "", err
			}
			if err := matched(ra); err != nil {
				return "", err
			}
			if err := matched(rb); err != nil {
				return "", err
			}
			return fmt.Sprintf("Alice saw %s, Bob saw %s, %s", ra.Match.Peer.DisplayName, rb.Match.Peer.DisplayName, time.Since(start).Round(time.Millisecond)), nil
		}},
		{"replay rejected", func(base string) (string, error) {
			ps, err := onboardAll(base, "Alice", "Bob")
			if err != nil {
				return "", err
			}
			sa, err := ps[0].StartTap()
			if err != nil {
				return "", err
			}
			sb, err := ps[1].StartTap()
			if err != nil {
				return "", err
			}
			ca, siga, _ := sa.Claim(sb.Nonce, sb.EphPub(), DefaultMeasurement())
			cb, sigb, _ := sb.Claim(sa.Nonce, sa.EphPub(), DefaultMeasurement())
			go func() { _, _ = ps[1].Submit(cb, sigb) }()
			if _, err := ps[0].Submit(ca, siga); err != nil {
				return "", err
			}
			sc, err := ps[1].StartTap()
			if err != nil {
				return "", err
			}
			replay, sig, _ := sa.Claim(sc.Nonce, sc.EphPub(), DefaultMeasurement())
			_, err = ps[0].Submit(replay, sig)
			if Code(err) != "nonce_used" {
				return "", fmt.Errorf("want nonce_used, got %v", err)
			}
			return "spent nonce refused (nonce_used)", nil
		}},
		{"relay across rooms rejected", func(base string) (string, error) {
			ps, err := onboardAll(base, "Alice", "Mallory")
			if err != nil {
				return "", err
			}
			far := DefaultMeasurement()
			far.DistanceCM = 400
			ra, _, err := Handshake(ps[0], ps[1], far, DefaultMeasurement())
			if err != nil {
				return "", err
			}
			if Code(ra.Err) != "too_far" {
				return "", fmt.Errorf("want too_far, got %v", ra.Err)
			}
			return "4 m measurement refused (too_far)", nil
		}},
		{"meetup forms an event", func(base string) (string, error) {
			ps, err := onboardAll(base, "Host", "Ana", "Ben", "Cai", "Dev", "Eve")
			if err != nil {
				return "", err
			}
			for _, guest := range ps[1:] {
				ra, _, err := Handshake(ps[0], guest, DefaultMeasurement(), DefaultMeasurement())
				if err != nil {
					return "", err
				}
				if err := matched(ra); err != nil {
					return "", err
				}
			}
			ev, err := ps[0].WaitEvent(5 * time.Second)
			if err != nil {
				return "", err
			}
			if !ev.Named {
				if err := ps[0].Do("PUT", "/v1/events/"+ev.ID, map[string]string{"name": "STL Go Meetup"}, nil); err != nil {
					return "", err
				}
			}
			evID := ev.ID
			var recap struct {
				Headline string `json:"headline"`
			}
			if err := ps[0].Do("GET", "/v1/events/"+evID+"/recap", nil, &recap); err != nil {
				return "", err
			}
			return recap.Headline, nil
		}},
		{"props week", func(base string) (string, error) {
			ps, err := onboardAll(base, "Ada", "Bo", "Cy", "Di", "Ed", "Flo")
			if err != nil {
				return "", err
			}
			for i := range ps {
				for j := i + 1; j < len(ps); j++ {
					ra, _, err := Handshake(ps[i], ps[j], DefaultMeasurement(), DefaultMeasurement())
					if err != nil {
						return "", err
					}
					if err := matched(ra); err != nil {
						return "", err
					}
				}
			}
			made := 0
			for _, p := range ps {
				n, err := p.AnswerProps(func(_ string, opts []Peer) int { return len(opts) - 1 })
				if err != nil {
					return "", fmt.Errorf("%s: %w", p.Name, err)
				}
				made += n
			}
			received, top := 0, ""
			for _, p := range ps {
				c, err := p.PropsCard()
				if err != nil {
					return "", err
				}
				if c.People > len(ps)-1 {
					return "", fmt.Errorf("%s picked by %d people but has %d connections", p.Name, c.People, len(ps)-1)
				}
				received += c.Picks
				if top == "" && len(c.ByPrompt) > 0 {
					top = fmt.Sprintf("%s: %q", p.Name, c.Headline)
				}
			}
			if received != made {
				return "", fmt.Errorf("%d picks made but %d received", made, received)
			}
			return fmt.Sprintf("%d picks made = %d received; %s", made, received, top), nil
		}},
		{fmt.Sprintf("burst of %d simultaneous taps", burstPairs), func(base string) (string, error) {
			return burst(base, burstPairs)
		}},
	}
}

func burst(base string, pairs int) (string, error) {
	var phones []*Phone
	for i := range pairs * 2 {
		p := NewPhone(base, fmt.Sprintf("Attendee%d", i))
		if err := p.Onboard(nextPhone()); err != nil {
			return "", err
		}
		phones = append(phones, p)
	}
	lat := make([]time.Duration, pairs)
	errs := make([]error, pairs)
	var wg sync.WaitGroup
	start := time.Now()
	for i := range pairs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t0 := time.Now()
			ra, rb, err := Handshake(phones[2*i], phones[2*i+1], DefaultMeasurement(), DefaultMeasurement())
			lat[i] = time.Since(t0)
			if err == nil {
				err = matched(ra)
			}
			if err == nil {
				err = matched(rb)
			}
			if err == nil && ra.Match.Receipt.TapID != rb.Match.Receipt.TapID {
				err = fmt.Errorf("pair %d got two different taps", i)
			}
			errs[i] = err
		}()
	}
	wg.Wait()
	total := time.Since(start)
	for _, err := range errs {
		if err != nil {
			return "", err
		}
	}
	slices.Sort(lat)
	return fmt.Sprintf("%d taps in %s, p50 %s, p99 %s", pairs, total.Round(time.Millisecond),
		lat[pairs/2].Round(time.Millisecond), lat[pairs*99/100].Round(time.Millisecond)), nil
}

// RunAll runs every scenario and reports to w; it returns an error if any
// scenario failed.
func RunAll(base string, burstPairs int, w io.Writer) error {
	failed := 0
	for _, sc := range Scenarios(burstPairs) {
		detail, err := sc.Run(base)
		if err != nil {
			failed++
			fmt.Fprintf(w, "FAIL  %-32s %v\n", sc.Name, err)
			continue
		}
		fmt.Fprintf(w, "ok    %-32s %s\n", sc.Name, detail)
	}
	if failed > 0 {
		return fmt.Errorf("%d scenario(s) failed", failed)
	}
	return nil
}
