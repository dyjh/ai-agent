package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
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
	root.AddCommand(runsCommand(&serverURL))
	root.AddCommand(codeCommand(&serverURL))
	root.AddCommand(gitCommand(&serverURL))
	root.AddCommand(opsCommand(&serverURL))

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
	installCmd := &cobra.Command{
		Use:   "install [zip_path]",
		Short: "Install a skill zip package",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			force, _ := c.Flags().GetBool("force")
			return printMultipartFileRequest(http.MethodPost, *serverURL+"/v1/skills/upload-zip", "file", args[0], map[string]string{
				"force": fmt.Sprintf("%t", force),
			})
		},
	}
	installCmd.Flags().Bool("force", false, "overwrite the same installed version")
	cmd.AddCommand(installCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List registered skills",
		RunE: func(_ *cobra.Command, _ []string) error {
			return printRequest(http.MethodGet, *serverURL+"/v1/skills", nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "manifest [skill_id]",
		Short: "Get the normalized manifest for one skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printRequest(http.MethodGet, fmt.Sprintf("%s/v1/skills/%s/manifest", *serverURL, args[0]), nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "package [skill_id]",
		Short: "Get package metadata for one skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printRequest(http.MethodGet, fmt.Sprintf("%s/v1/skills/%s/package", *serverURL, args[0]), nil)
		},
	})
	validateCmd := &cobra.Command{
		Use:   "validate [skill_id]",
		Short: "Validate a skill without executing it",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			payload, err := argsFlagPayload(c)
			if err != nil {
				return err
			}
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/skills/%s/validate", *serverURL, args[0]), payload)
		},
	}
	validateCmd.Flags().String("args", "{}", "JSON object passed as skill args")
	cmd.AddCommand(validateCmd)
	testCmd := &cobra.Command{
		Use:   "test [skill_id]",
		Short: "Validate skill input compatibility",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			payload, err := argsFlagPayload(c)
			if err != nil {
				return err
			}
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/skills/%s/test", *serverURL, args[0]), payload)
		},
	}
	testCmd.Flags().String("args", "{}", "JSON object passed as skill args")
	cmd.AddCommand(testCmd)
	runCmd := &cobra.Command{
		Use:   "run [skill_id]",
		Short: "Run a registered skill through the approval chain",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			payload, err := argsFlagPayload(c)
			if err != nil {
				return err
			}
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/skills/%s/run", *serverURL, args[0]), payload)
		},
	}
	runCmd.Flags().String("args", "{}", "JSON object passed as skill args")
	cmd.AddCommand(runCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "enable [skill_id]",
		Short: "Enable a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/skills/%s/enable", *serverURL, args[0]), map[string]any{})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "disable [skill_id]",
		Short: "Disable a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/skills/%s/disable", *serverURL, args[0]), map[string]any{})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "remove [skill_id]",
		Short: "Remove a registered skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printRequest(http.MethodDelete, fmt.Sprintf("%s/v1/skills/%s", *serverURL, args[0]), nil)
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

func runsCommand(serverURL *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Inspect and manage workflow runs",
	}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List recent runs",
		RunE: func(c *cobra.Command, _ []string) error {
			status, _ := c.Flags().GetString("status")
			limit, _ := c.Flags().GetInt("limit")
			url := fmt.Sprintf("%s/v1/runs?limit=%d", *serverURL, limit)
			if status != "" {
				url += "&status=" + status
			}
			return printRequest(http.MethodGet, url, nil)
		},
	}
	listCmd.Flags().String("status", "", "filter by status")
	listCmd.Flags().Int("limit", 20, "max runs to return")
	cmd.AddCommand(listCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "get [run_id]",
		Short: "Get one run snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printRequest(http.MethodGet, fmt.Sprintf("%s/v1/runs/%s", *serverURL, args[0]), nil)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "steps [run_id]",
		Short: "List step history for a run",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printRequest(http.MethodGet, fmt.Sprintf("%s/v1/runs/%s/steps", *serverURL, args[0]), nil)
		},
	})
	resumeCmd := &cobra.Command{
		Use:   "resume [run_id]",
		Short: "Resume a paused run",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			approvalID, _ := c.Flags().GetString("approval")
			approved, _ := c.Flags().GetBool("approved")
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/runs/%s/resume", *serverURL, args[0]), map[string]any{
				"approval_id": approvalID,
				"approved":    approved,
			})
		},
	}
	resumeCmd.Flags().String("approval", "", "approval ID to resolve")
	resumeCmd.Flags().Bool("approved", true, "whether to approve the pending action")
	cmd.AddCommand(resumeCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "cancel [run_id]",
		Short: "Cancel a non-terminal run",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/runs/%s/cancel", *serverURL, args[0]), map[string]any{})
		},
	})
	return cmd
}

func codeCommand(serverURL *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "code",
		Short: "Use code tools through the agent workflow",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "inspect [workspace]",
		Short: "Inspect a workspace through code.inspect_project",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请检查项目结构和测试命令，workspace: %s", args[0]))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "search [workspace] [query]",
		Short: "Search code through code.search_text",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请在 workspace %s 搜索代码 `%s`", args[0], args[1]))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "read [workspace] [path]",
		Short: "Read a file through code.read_file",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请读取文件 `%s`，workspace: %s", args[1], args[0]))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "test [workspace]",
		Short: "Run detected tests through code.run_tests",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请检测并运行测试，workspace: %s", args[0]))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "fix-tests [workspace]",
		Short: "Run the bounded test repair loop through code.fix_test_failure_loop",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请修复测试失败并进入有界修复循环，workspace: %s", args[0]))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "diff [workspace]",
		Short: "Show git diff through the agent workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请查看 git diff，workspace: %s", args[0]))
		},
	})
	patchCmd := &cobra.Command{
		Use:   "patch",
		Short: "Patch utilities routed through the agent workflow",
	}
	patchCmd.AddCommand(&cobra.Command{
		Use:   "validate [patch_file]",
		Short: "Ask the agent to validate a patch file",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请验证 patch 文件 `%s`，只做 dry-run，不要应用。", args[0]))
		},
	})
	patchCmd.AddCommand(&cobra.Command{
		Use:   "dry-run [patch_file]",
		Short: "Dry-run a patch file through code.validate_patch",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请 dry-run patch 文件 `%s`，不要应用。", args[0]))
		},
	})
	patchCmd.AddCommand(&cobra.Command{
		Use:   "apply [patch_file]",
		Short: "Request approval to apply a patch file through code.apply_patch",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请应用 patch 文件 `%s`，必须先请求审批。", args[0]))
		},
	})
	cmd.AddCommand(patchCmd)
	return cmd
}

func gitCommand(serverURL *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "git",
		Short: "Use git tools through the agent workflow",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status [workspace]",
		Short: "Run git.status through the workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请查看 git status，workspace: %s", args[0]))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "diff [workspace]",
		Short: "Run git.diff through the workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请查看 git diff，workspace: %s", args[0]))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "diff-summary [workspace]",
		Short: "Run git.diff_summary through the workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请总结 git diff，workspace: %s", args[0]))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "commit-message [workspace]",
		Short: "Propose a commit message without committing",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请生成 commit message 建议但不要提交，workspace: %s", args[0]))
		},
	})
	return cmd
}

func opsCommand(serverURL *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ops",
		Short: "Use operations tools through the agent workflow",
	}
	hostsCmd := &cobra.Command{
		Use:   "hosts",
		Short: "Manage operations host profiles",
	}
	hostsCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List host profiles",
		RunE: func(_ *cobra.Command, _ []string) error {
			return printRequest(http.MethodGet, *serverURL+"/v1/ops/hosts", nil)
		},
	})
	addHostCmd := &cobra.Command{
		Use:   "add",
		Short: "Add a local host profile",
		RunE: func(c *cobra.Command, _ []string) error {
			name, _ := c.Flags().GetString("name")
			hostType, _ := c.Flags().GetString("type")
			workspace, _ := c.Flags().GetString("working-directory")
			return printJSONRequest(http.MethodPost, *serverURL+"/v1/ops/hosts", map[string]any{
				"name":              name,
				"type":              hostType,
				"working_directory": workspace,
			})
		},
	}
	addHostCmd.Flags().String("name", "Localhost", "host profile name")
	addHostCmd.Flags().String("type", "local", "host type")
	addHostCmd.Flags().String("working-directory", ".", "working directory")
	hostsCmd.AddCommand(addHostCmd)
	addSSHCmd := &cobra.Command{
		Use:   "add-ssh",
		Short: "Add an SSH host profile without storing secrets",
		RunE: func(c *cobra.Command, _ []string) error {
			name, _ := c.Flags().GetString("name")
			host, _ := c.Flags().GetString("host")
			user, _ := c.Flags().GetString("user")
			port, _ := c.Flags().GetInt("port")
			authType, _ := c.Flags().GetString("auth-type")
			keyPath, _ := c.Flags().GetString("key-path")
			passwordRef, _ := c.Flags().GetString("password-ref")
			return printJSONRequest(http.MethodPost, *serverURL+"/v1/ops/hosts", map[string]any{
				"name": name,
				"type": "ssh",
				"ssh": map[string]any{
					"host":         host,
					"user":         user,
					"port":         port,
					"auth_type":    authType,
					"key_path":     keyPath,
					"password_ref": passwordRef,
				},
			})
		},
	}
	addSSHCmd.Flags().String("name", "SSH host", "host profile name")
	addSSHCmd.Flags().String("host", "", "SSH hostname")
	addSSHCmd.Flags().String("user", "", "SSH user")
	addSSHCmd.Flags().Int("port", 22, "SSH port")
	addSSHCmd.Flags().String("auth-type", "agent", "SSH auth type: agent, key, password-ref")
	addSSHCmd.Flags().String("key-path", "", "SSH key path; value is redacted in API responses")
	addSSHCmd.Flags().String("password-ref", "", "external password reference; value is redacted in API responses")
	hostsCmd.AddCommand(addSSHCmd)
	hostsCmd.AddCommand(&cobra.Command{
		Use:   "get [host_id]",
		Short: "Get one host profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printRequest(http.MethodGet, fmt.Sprintf("%s/v1/ops/hosts/%s", *serverURL, args[0]), nil)
		},
	})
	hostsCmd.AddCommand(&cobra.Command{
		Use:   "test [host_id]",
		Short: "Test one host profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/ops/hosts/%s/test", *serverURL, args[0]), map[string]any{})
		},
	})
	hostsCmd.AddCommand(&cobra.Command{
		Use:   "remove [host_id]",
		Short: "Remove one host profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printRequest(http.MethodDelete, fmt.Sprintf("%s/v1/ops/hosts/%s", *serverURL, args[0]), nil)
		},
	})
	cmd.AddCommand(hostsCmd)

	sshCmd := &cobra.Command{Use: "ssh", Short: "Run SSH ops through the workflow"}
	sshCmd.AddCommand(&cobra.Command{
		Use:   "processes [host_id]",
		Short: "Inspect remote processes",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请查看 SSH host %s 的进程和 CPU 占用", args[0]))
		},
	})
	sshCmd.AddCommand(&cobra.Command{
		Use:   "logs [host_id] [path]",
		Short: "Tail remote logs",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请查看 SSH host %s 的日志 `%s`", args[0], args[1]))
		},
	})
	cmd.AddCommand(sshCmd)

	dockerCmd := &cobra.Command{Use: "docker", Short: "Run Docker ops through the workflow"}
	dockerCmd.AddCommand(&cobra.Command{
		Use:   "ps",
		Short: "List Docker containers",
		RunE: func(_ *cobra.Command, _ []string) error {
			return askWorkflow(*serverURL, "请查看 docker 容器状态")
		},
	})
	dockerCmd.AddCommand(&cobra.Command{
		Use:   "logs [container]",
		Short: "Read Docker logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请查看 docker container `%s` 的日志", args[0]))
		},
	})
	dockerCmd.AddCommand(&cobra.Command{
		Use:   "stats",
		Short: "Read Docker stats",
		RunE: func(_ *cobra.Command, _ []string) error {
			return askWorkflow(*serverURL, "请查看 docker stats")
		},
	})
	dockerCmd.AddCommand(&cobra.Command{
		Use:   "restart [container]",
		Short: "Request approval to restart a Docker container",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请重启 docker container `%s`，必须审批", args[0]))
		},
	})
	cmd.AddCommand(dockerCmd)

	k8sCmd := &cobra.Command{Use: "k8s", Short: "Run Kubernetes ops through the workflow"}
	k8sCmd.AddCommand(&cobra.Command{
		Use:   "get [resource]",
		Short: "Get Kubernetes resources",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请查看 k8s get %s", args[0]))
		},
	})
	k8sCmd.AddCommand(&cobra.Command{
		Use:   "logs [target]",
		Short: "Read Kubernetes logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请查看 k8s pod `%s` 日志", args[0]))
		},
	})
	k8sCmd.AddCommand(&cobra.Command{
		Use:   "describe [resource] [name]",
		Short: "Describe a Kubernetes resource",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请 describe k8s %s `%s`", args[0], args[1]))
		},
	})
	k8sCmd.AddCommand(&cobra.Command{
		Use:   "apply [manifest]",
		Short: "Request approval to apply a Kubernetes manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return askWorkflow(*serverURL, fmt.Sprintf("请 k8s apply `%s`，必须审批", args[0]))
		},
	})
	cmd.AddCommand(k8sCmd)

	runbooksCmd := &cobra.Command{Use: "runbooks", Short: "Manage operations runbooks"}
	runbooksCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List runbooks",
		RunE: func(_ *cobra.Command, _ []string) error {
			return printRequest(http.MethodGet, *serverURL+"/v1/ops/runbooks", nil)
		},
	})
	runbooksCmd.AddCommand(&cobra.Command{
		Use:   "read [id]",
		Short: "Read a runbook",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printRequest(http.MethodGet, fmt.Sprintf("%s/v1/ops/runbooks/%s", *serverURL, args[0]), nil)
		},
	})
	runbooksCmd.AddCommand(&cobra.Command{
		Use:   "plan [id]",
		Short: "Plan a runbook",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/ops/runbooks/%s/plan", *serverURL, args[0]), map[string]any{"dry_run": true})
		},
	})
	runbooksCmd.AddCommand(&cobra.Command{
		Use:   "execute [id]",
		Short: "Execute a runbook through ToolRouter",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/ops/runbooks/%s/execute", *serverURL, args[0]), map[string]any{"max_steps": 5})
		},
	})
	cmd.AddCommand(runbooksCmd)
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

func askWorkflow(serverURL, message string) error {
	convID, err := createConversation(serverURL, "")
	if err != nil {
		return err
	}
	return printJSONRequest(http.MethodPost, fmt.Sprintf("%s/v1/conversations/%s/messages", serverURL, convID), map[string]any{
		"content": message,
	})
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

func printMultipartFileRequest(method, url, fieldName, path string, fields map[string]string) error {
	respBody, err := doMultipartFileRequest(method, url, fieldName, path, fields)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, string(respBody))
	return err
}

func doMultipartFileRequest(method, url, fieldName, path string, fields map[string]string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, err
		}
	}
	part, err := writer.CreateFormFile(fieldName, filepathBase(path))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, url, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func argsFlagPayload(cmd *cobra.Command) (map[string]any, error) {
	rawArgs, err := cmd.Flags().GetString("args")
	if err != nil {
		return nil, err
	}
	args := map[string]any{}
	if strings.TrimSpace(rawArgs) != "" {
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return nil, fmt.Errorf("--args must be a JSON object: %w", err)
		}
	}
	return map[string]any{"args": args}, nil
}

func filepathBase(path string) string {
	parts := strings.Split(strings.ReplaceAll(path, "\\", "/"), "/")
	return parts[len(parts)-1]
}
