package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/store"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/web"
	"github.com/rohithIlluri/POC-S/pocs/vitals/internal/whoop"
)

func (a *app) cmdWhoop(args []string) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	switch args[0] {
	case "setup":
		return a.whoopSetup(args[1:])
	case "login", "connect":
		return a.whoopLogin(args[1:])
	case "sync":
		return a.whoopSync(args[1:])
	case "status":
		return a.whoopStatus()
	case "logout", "disconnect":
		return a.whoopLogout()
	default:
		return fmt.Errorf("unknown whoop command %q (setup, login, sync, status, logout)", args[0])
	}
}

func (a *app) webServer() *web.Server {
	return &web.Server{Home: a.home, Loc: a.loc, Now: a.now, WhoopBase: a.whoopBase, HTTP: a.http}
}

func (a *app) whoopClient(c whoop.Credentials) *whoop.Client {
	cl := whoop.NewClient(c, func(c whoop.Credentials) error { return whoop.SaveCredentials(a.home, c) })
	cl.Base, cl.Now = a.whoopBase, a.now
	if a.http != nil {
		cl.HTTP = a.http
	}
	return cl
}

func (a *app) readLine(prompt string) (string, error) {
	fmt.Fprint(a.err, prompt)
	// One buffered reader for the whole run: a fresh one per prompt would
	// swallow the lines meant for later prompts when input is piped.
	if a.stdin == nil {
		a.stdin = bufio.NewReader(a.in)
	}
	line, err := a.stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no input")
	}
	return strings.TrimSpace(line), nil
}

func (a *app) whoopSetup(args []string) error {
	fs := a.flags("whoop setup")
	id := fs.String("client-id", "", "Client ID of your WHOOP developer app")
	secret := fs.String("client-secret", "", "Client Secret (omit to be prompted, keeping it out of shell history)")
	redirect := fs.String("redirect", "", "redirect URL registered on the app (default "+whoop.DefaultRedirect+")")
	if _, err := parseInterleaved(fs, args); err != nil {
		return err
	}
	c, err := whoop.LoadCredentials(a.home)
	if err != nil {
		return err
	}
	if *id != "" {
		c.ClientID = strings.TrimSpace(*id)
	}
	if *redirect != "" {
		u, err := url.Parse(*redirect)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("--redirect %q is not an absolute URL", *redirect)
		}
		c.RedirectURL = *redirect
	}
	if c.ClientID == "" {
		if c.ClientID, err = a.readLine("WHOOP Client ID: "); err != nil {
			return err
		}
	}
	switch {
	case *secret != "":
		c.ClientSecret = strings.TrimSpace(*secret)
	case c.ClientSecret == "" || *id != "":
		if c.ClientSecret, err = a.readLine("WHOOP Client Secret: "); err != nil {
			return err
		}
	}
	if !c.Configured() {
		return errors.New("both a client ID and a client secret are needed")
	}
	if err := whoop.SaveCredentials(a.home, c); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "saved WHOOP app credentials to %s (readable only by you)\n", a.home+string(os.PathSeparator)+whoop.FileName)
	fmt.Fprintf(a.out, "make sure this redirect URL is registered on the app: %s\n", c.RedirectURL)
	fmt.Fprintln(a.out, "next: vitals whoop login")
	return nil
}

func (a *app) whoopLogin(args []string) error {
	fs := a.flags("whoop login")
	paste := fs.Bool("paste", false, "no local callback: open the URL anywhere, then paste back the address you land on (for SSH / remote machines)")
	noBrowser := fs.Bool("no-browser", false, "print the login URL instead of opening a browser")
	timeout := fs.Duration("timeout", 5*time.Minute, "how long to wait for the browser login")
	if _, err := parseInterleaved(fs, args); err != nil {
		return err
	}
	c, err := whoop.LoadCredentials(a.home)
	if err != nil {
		return err
	}
	if !c.Configured() {
		return errors.New("no WHOOP app credentials yet — run `vitals whoop setup` first")
	}
	if *paste {
		return a.whoopLoginPaste(c)
	}

	u, err := url.Parse(c.RedirectURL)
	if err != nil || !web.IsLoopbackHost(u.Host) || u.Port() == "" {
		return fmt.Errorf("redirect URL %s isn't a localhost address with a port; use `vitals whoop login --paste`", c.RedirectURL)
	}
	ls, err := web.Listen(u.Host)
	if err != nil {
		return fmt.Errorf("can't listen for the WHOOP callback on %s (is `vitals serve` already running? then use its Connect button): %w", u.Host, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	srv := a.webServer()
	var member string
	connected := false
	srv.OnConnected = func(m string) {
		member, connected = m, true
		// Give the browser a moment to follow the final redirect.
		time.AfterFunc(1500*time.Millisecond, cancel)
	}
	start := (&url.URL{Scheme: "http", Host: u.Host, Path: "/whoop/connect"}).String()
	fmt.Fprintf(a.out, "Log in to WHOOP in your browser: %s\n", start)
	if !*noBrowser {
		openBrowser(start)
	}
	fmt.Fprintln(a.out, "waiting for WHOOP to send you back… (Ctrl-C to cancel)")
	if err := web.Serve(ctx, srv.Handler(), ls); err != nil {
		return err
	}
	if !connected {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("timed out waiting for the WHOOP login")
		}
		return errors.New("WHOOP login cancelled")
	}
	fmt.Fprintf(a.out, "connected to WHOOP%s. next: vitals whoop sync\n", asName(member))
	return nil
}

func asName(m string) string {
	if m == "" {
		return ""
	}
	return " as " + m
}

// whoopLoginPaste is for machines that can't receive the localhost
// callback: the browser lands on a page that fails to load, and its address
// bar holds the code.
func (a *app) whoopLoginPaste(c whoop.Credentials) error {
	state := whoop.NewState()
	fmt.Fprintf(a.out, "1. Open this URL in any browser and approve vitals:\n\n   %s\n\n", whoop.AuthURL(a.whoopBase, c, state))
	fmt.Fprintf(a.out, "2. WHOOP redirects to %s… — the page won't load; that's expected.\n   Copy the whole address from the address bar.\n\n", c.RedirectURL)
	line, err := a.readLine("Paste it here: ")
	if err != nil {
		return err
	}
	u, err := url.Parse(line)
	if err != nil || u.Query().Get("code") == "" {
		return errors.New("that doesn't look like the redirect address (it should contain ?code=…)")
	}
	q := u.Query()
	if e := q.Get("error"); e != "" {
		return fmt.Errorf("WHOOP login was not completed (%s)", e)
	}
	if q.Get("state") != state {
		return errors.New("state mismatch: that address is from a different login attempt — run the command again")
	}
	member, err := a.webServer().CompleteLogin(context.Background(), q.Get("code"))
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "connected to WHOOP%s. next: vitals whoop sync\n", asName(member))
	return nil
}

func (a *app) whoopSync(args []string) error {
	fs := a.flags("whoop sync")
	days := fs.Int("days", 0, "re-sync this many days back (default: since the last sync, or 30 days the first time)")
	if _, err := parseInterleaved(fs, args); err != nil {
		return err
	}
	if *days < 0 || *days > 3650 {
		return errors.New("--days must be between 1 and 3650")
	}
	c, err := whoop.LoadCredentials(a.home)
	if err != nil {
		return err
	}
	if !c.Connected() {
		return whoop.ErrNotConnected
	}
	st, err := store.Open(a.home)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	res, err := whoop.Sync(ctx, a.whoopClient(c), st, *days)
	if err != nil {
		return err
	}
	fmt.Fprintln(a.out, web.SyncSummary(res))
	return nil
}

func (a *app) whoopStatus() error {
	c, err := whoop.LoadCredentials(a.home)
	if err != nil {
		return err
	}
	switch {
	case !c.Configured():
		fmt.Fprintln(a.out, "WHOOP: not set up.")
		fmt.Fprintln(a.out, "  1. Create an app at https://developer.whoop.com (Dashboard → Create app)")
		fmt.Fprintf(a.out, "     with redirect URL %s and all read scopes.\n", c.RedirectURL)
		fmt.Fprintln(a.out, "  2. vitals whoop setup --client-id <id>     (prompts for the secret)")
		fmt.Fprintln(a.out, "  3. vitals whoop login")
	case !c.Connected():
		fmt.Fprintln(a.out, "WHOOP: app credentials saved, not connected — run `vitals whoop login`.")
	default:
		fmt.Fprintf(a.out, "WHOOP: connected%s\n", asName(c.MemberName))
		if c.LastSync.IsZero() {
			fmt.Fprintln(a.out, "  never synced — run `vitals whoop sync`")
		} else {
			fmt.Fprintf(a.out, "  last sync %s\n", c.LastSync.In(a.loc).Format("Mon 2 Jan 15:04"))
		}
	}
	return nil
}

func (a *app) whoopLogout() error {
	c, err := whoop.LoadCredentials(a.home)
	if err != nil {
		return err
	}
	if !c.Connected() {
		fmt.Fprintln(a.out, "not connected")
		return nil
	}
	if err := a.whoopClient(c).Revoke(context.Background()); err != nil {
		fmt.Fprintf(a.err, "vitals: warning: couldn't revoke access at WHOOP (%v); forgetting the tokens locally anyway\n", err)
	}
	c.Disconnect()
	if err := whoop.SaveCredentials(a.home, c); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "disconnected from WHOOP. synced entries stay in your log; app credentials are kept for next time.")
	return nil
}

func (a *app) cmdServe(args []string) error {
	fs := a.flags("serve")
	addr := fs.String("addr", "localhost:8737", "loopback address to listen on")
	open := fs.Bool("open", false, "open the dashboard in a browser")
	if _, err := parseInterleaved(fs, args); err != nil {
		return err
	}
	ls, err := web.Listen(*addr)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	link := "http://" + *addr + "/"
	fmt.Fprintf(a.out, "vitals dashboard on %s (local only) — Ctrl-C to stop\n", link)
	if *open {
		openBrowser(link)
	}
	return web.Serve(ctx, a.webServer().Handler(), ls)
}

// openBrowser is best-effort; the URL is always printed too.
func openBrowser(link string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", link)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", link)
	default:
		cmd = exec.Command("xdg-open", link)
	}
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}
