package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"nhooyr.io/websocket"

	"local-agent-ui-tui/internal/client"
	"local-agent-ui-tui/internal/ui"
)

func main() {
	defaultServer := os.Getenv("AGENT_API_BASE_URL")
	if defaultServer == "" {
		defaultServer = "http://127.0.0.1:8765"
	}
	server := flag.String("server", defaultServer, "agent server base URL")
	flag.Parse()

	ctx := context.Background()
	app := &session{client: client.New(*server)}
	if err := app.ensureConversation(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "startup error: %v\n", err)
		os.Exit(1)
	}
	app.loop(ctx)
}

type session struct {
	client       client.Client
	conversation string
	conn         *websocket.Conn
}

func (s *session) ensureConversation(ctx context.Context) error {
	if s.conversation == "" {
		id, err := s.client.CreateConversation(ctx)
		if err != nil {
			return err
		}
		s.conversation = id
	}
	conn, err := s.client.ChatSocket(ctx, s.conversation)
	if err != nil {
		fmt.Printf("websocket unavailable, HTTP fallback active: %v\n", err)
		return nil
	}
	s.conn = conn
	go s.readEvents(ctx)
	fmt.Printf("conversation: %s\n", s.conversation)
	return nil
}

func (s *session) readEvents(ctx context.Context) {
	for {
		event, err := client.ReadEvent(ctx, s.conn)
		if err != nil {
			fmt.Printf("\nwebsocket closed: %v\n", err)
			return
		}
		fmt.Print(ui.RenderEvent(event))
		if event.Type == "assistant.delta" {
			continue
		}
		fmt.Print("\n> ")
	}
}

func (s *session) loop(ctx context.Context) {
	fmt.Println("Local Agent TUI. Type :help for commands.")
	fmt.Print("> ")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Print("> ")
			continue
		}
		if strings.HasPrefix(line, ":") {
			if s.command(ctx, line) {
				return
			}
			fmt.Print("> ")
			continue
		}
		s.send(ctx, line)
		fmt.Print("> ")
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "input error: %v\n", err)
	}
}

func (s *session) send(ctx context.Context, line string) {
	if s.conn != nil {
		if err := client.SendUserMessage(ctx, s.conn, line); err == nil {
			return
		}
	}
	resp, err := s.client.PostMessage(ctx, s.conversation, line)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Println(ui.RenderJSON(resp))
}

func (s *session) command(ctx context.Context, line string) bool {
	args := strings.Fields(line)
	switch args[0] {
	case ":quit", ":exit":
		if s.conn != nil {
			_ = s.conn.Close(websocket.StatusNormalClosure, "bye")
		}
		return true
	case ":help":
		fmt.Println(":new | :approvals | :approve <id> | :reject <id> [reason] | :runs | :run <id> | :memory <query> | :kb <kb_id> <question> | :evals | :diff | :quit")
	case ":new":
		if s.conn != nil {
			_ = s.conn.Close(websocket.StatusNormalClosure, "new conversation")
		}
		s.conversation = ""
		if err := s.ensureConversation(ctx); err != nil {
			fmt.Printf("error: %v\n", err)
		}
	case ":approvals":
		s.printJSON(s.client.PendingApprovals(ctx))
	case ":approve":
		if len(args) < 2 {
			fmt.Println("usage: :approve <approval_id>")
			return false
		}
		if s.conn != nil {
			_ = client.SendApprovalResponse(ctx, s.conn, args[1], true)
			return false
		}
		s.printJSON(s.client.Approve(ctx, args[1]))
	case ":reject":
		if len(args) < 2 {
			fmt.Println("usage: :reject <approval_id> [reason]")
			return false
		}
		reason := "rejected from TUI"
		if len(args) > 2 {
			reason = strings.Join(args[2:], " ")
		}
		if s.conn != nil {
			_ = client.SendApprovalResponse(ctx, s.conn, args[1], false)
			return false
		}
		s.printJSON(s.client.Reject(ctx, args[1], reason))
	case ":runs":
		s.printJSON(s.client.Runs(ctx))
	case ":run":
		if len(args) < 2 {
			fmt.Println("usage: :run <run_id>")
			return false
		}
		s.printJSON(s.client.RunSteps(ctx, args[1]))
	case ":memory":
		if len(args) < 2 {
			fmt.Println("usage: :memory <query>")
			return false
		}
		s.printJSON(s.client.MemorySearch(ctx, strings.Join(args[1:], " ")))
	case ":kb":
		if len(args) < 3 {
			fmt.Println("usage: :kb <kb_id> <question>")
			return false
		}
		s.printJSON(s.client.KBAnswer(ctx, args[1], strings.Join(args[2:], " ")))
	case ":evals":
		s.printJSON(s.client.EvalRuns(ctx))
	case ":diff":
		fmt.Println("paste unified diff, finish with a single '.' line")
		diff := readMultiline()
		fmt.Println(ui.PreviewDiff(diff))
	default:
		fmt.Println("unknown command")
	}
	return false
}

func (s *session) printJSON(value map[string]any, err error) {
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	fmt.Println(ui.RenderJSON(value))
}

func readMultiline() string {
	scanner := bufio.NewScanner(os.Stdin)
	var lines []string
	deadline := time.Now().Add(5 * time.Minute)
	for scanner.Scan() {
		if time.Now().After(deadline) {
			break
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == "." {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
