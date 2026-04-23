package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func main() {
	serverURL := "http://127.0.0.1:8765"
	root := &cobra.Command{
		Use:   "agent",
		Short: "CLI for the local agent server",
	}
	root.PersistentFlags().StringVar(&serverURL, "server", serverURL, "agent server base URL")

	root.AddCommand(healthCommand(&serverURL))
	root.AddCommand(askCommand(&serverURL))
	root.AddCommand(chatCommand(&serverURL))
	root.AddCommand(approvalsCommand(&serverURL))
	root.AddCommand(memoryCommand(&serverURL))
	root.AddCommand(skillsCommand(&serverURL))
	root.AddCommand(mcpCommand(&serverURL))
	root.AddCommand(kbCommand(&serverURL))

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func healthCommand(serverURL *string) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Query server health",
		RunE: func(_ *cobra.Command, _ []string) error {
			return printRequest(http.MethodGet, *serverURL+"/v1/health", nil)
		},
	}
}

func askCommand(serverURL *string) *cobra.Command {
	return &cobra.Command{
		Use:   "ask [message]",
		Short: "Create a conversation and send one message",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			convID, err := createConversation(*serverURL, "")
			if err != nil {
				return err
			}
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/conversations/%s/messages", *serverURL, convID), map[string]any{
				"content": strings.Join(args, " "),
			})
		},
	}
}

func chatCommand(serverURL *string) *cobra.Command {
	var conversationID string
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Open a simple interactive chat loop",
		RunE: func(_ *cobra.Command, _ []string) error {
			if conversationID == "" {
				convID, err := createConversation(*serverURL, "")
				if err != nil {
					return err
				}
				conversationID = convID
			}

			fmt.Fprintf(os.Stdout, "conversation: %s\n", conversationID)
			scanner := bufio.NewScanner(os.Stdin)
			for {
				fmt.Fprint(os.Stdout, "> ")
				if !scanner.Scan() {
					return scanner.Err()
				}
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				if line == "/exit" || line == "/quit" {
					return nil
				}
				if err := printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/conversations/%s/messages", *serverURL, conversationID), map[string]any{
					"content": line,
				}); err != nil {
					return err
				}
			}
		},
	}
	cmd.Flags().StringVar(&conversationID, "conversation", "", "existing conversation ID")
	return cmd
}

func approvalsCommand(serverURL *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approvals",
		Short: "Manage approvals",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List pending approvals",
		RunE: func(_ *cobra.Command, _ []string) error {
			return printRequest(http.MethodGet, *serverURL+"/v1/approvals/pending", nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "approve [approval_id]",
		Short: "Approve and execute a pending approval",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/approvals/%s/approve", *serverURL, args[0]), map[string]any{})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "reject [approval_id]",
		Short: "Reject a pending approval",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/approvals/%s/reject", *serverURL, args[0]), map[string]any{
				"reason": "rejected from CLI",
			})
		},
	})
	return cmd
}

func memoryCommand(serverURL *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage markdown memory",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List memory files",
		RunE: func(_ *cobra.Command, _ []string) error {
			return printRequest(http.MethodGet, *serverURL+"/v1/memory/files", nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "search [query]",
		Short: "Search memory files",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printJSONRequest(http.MethodPost, *serverURL+"/v1/memory/search", map[string]any{
				"query": args[0],
				"limit": 5,
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "reindex",
		Short: "Reindex memory into the vector index",
		RunE: func(_ *cobra.Command, _ []string) error {
			return printJSONRequest(http.MethodPost, *serverURL+"/v1/memory/reindex", map[string]any{})
		},
	})
	return cmd
}

func skillsCommand(serverURL *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage local skills",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "upload [path]",
		Short: "Register a local skill archive or directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printJSONRequest(http.MethodPost, *serverURL+"/v1/skills/upload", map[string]any{
				"path": args[0],
				"name": filepathBase(args[0]),
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List registered skills",
		RunE: func(_ *cobra.Command, _ []string) error {
			return printRequest(http.MethodGet, *serverURL+"/v1/skills", nil)
		},
	})
	return cmd
}

func mcpCommand(serverURL *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Manage MCP configuration",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List MCP servers",
		RunE: func(_ *cobra.Command, _ []string) error {
			return printRequest(http.MethodGet, *serverURL+"/v1/mcp/servers", nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "tools",
		Short: "List MCP tool policy overrides",
		RunE: func(_ *cobra.Command, _ []string) error {
			return printRequest(http.MethodGet, *serverURL+"/v1/mcp/tools", nil)
		},
	})
	return cmd
}

func kbCommand(serverURL *string) *cobra.Command {
	var kbID string
	cmd := &cobra.Command{
		Use:   "kb",
		Short: "Manage the knowledge base",
	}
	cmd.PersistentFlags().StringVar(&kbID, "kb-id", "", "knowledge base ID")
	cmd.AddCommand(&cobra.Command{
		Use:   "upload [path]",
		Short: "Upload a local file into a KB",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if kbID == "" {
				return fmt.Errorf("--kb-id is required")
			}
			content, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/kbs/%s/documents/upload", *serverURL, kbID), map[string]any{
				"filename": filepathBase(args[0]),
				"content":  string(content),
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "search [query]",
		Short: "Search a KB",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if kbID == "" {
				return fmt.Errorf("--kb-id is required")
			}
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/kbs/%s/search", *serverURL, kbID), map[string]any{
				"query": args[0],
				"limit": 5,
			})
		},
	})
	return cmd
}

func createConversation(serverURL, title string) (string, error) {
	respBody, err := doJSONRequest(http.MethodPost, serverURL+"/v1/conversations", map[string]any{"title": title})
	if err != nil {
		return "", err
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return "", err
	}
	return payload.ID, nil
}

func printJSONRequest(method, url string, body any) error {
	respBody, err := doJSONRequest(method, url, body)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(respBody))
	return err
}

func printRequest(method, url string, body io.Reader) error {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(data))
	return err
}

func doJSONRequest(method, url string, body any) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func filepathBase(path string) string {
	parts := strings.Split(strings.ReplaceAll(path, "\\", "/"), "/")
	return parts[len(parts)-1]
}
