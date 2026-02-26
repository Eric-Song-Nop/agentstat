package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Eric-Song-Nop/agentstat/internal/model"
	"github.com/Eric-Song-Nop/agentstat/internal/platform"
)

// geminiSessionFile holds a parsed Gemini session JSON file with its mtime.
type geminiSessionFile struct {
	Path       string
	ModTime    time.Time
	ProjectDir string // parent of "chats" directory, e.g. ~/.gemini/tmp/{project}
	Data       geminiSession
}

// geminiSession is the top-level structure of ~/.gemini/tmp/{project}/chats/session-*.json.
type geminiSession struct {
	SessionID   string          `json:"sessionId"`
	StartTime   string          `json:"startTime"`
	LastUpdated string          `json:"lastUpdated"`
	Messages    []geminiMessage `json:"messages"`
}

// geminiMessage represents a single message in the Gemini session.
type geminiMessage struct {
	Type      string `json:"type"`      // "user" | "gemini" | "error" | "info"
	Timestamp string `json:"timestamp"`
}

// DiscoverGemini finds all running Gemini CLI processes and determines their status.
//
// Gemini spawns a child node process with identical argv for each session. We filter
// children by checking PPID membership in the PID set, then group parent PIDs by CWD
// and pair them with matching session files ordered by startTime.
func DiscoverGemini() []model.AgentSession {
	pids := findGeminiPIDs()
	if len(pids) == 0 {
		return nil
	}

	// Filter out child processes whose PPID is also a Gemini PID.
	parentPIDs := filterGeminiParents(pids)

	sessions := loadGeminiSessions()
	if len(sessions) == 0 {
		// Processes running but no session files — report unknown.
		var results []model.AgentSession
		for _, pid := range parentPIDs {
			cwd := platform.P.ReadProcessCwd(pid)
			results = append(results, model.AgentSession{
				Agent:     "gemini",
				Status:    model.StatusUnknown,
				Directory: cwd,
				PID:       pid,
			})
		}
		return results
	}

	// Group parent PIDs by CWD, then pair each group with matching sessions.
	type pidCwd struct {
		PID int
		CWD string
	}
	var entries []pidCwd
	for _, pid := range parentPIDs {
		cwd := platform.P.ReadProcessCwd(pid)
		if cwd == "" || cwd == "-" {
			continue
		}
		entries = append(entries, pidCwd{PID: pid, CWD: cwd})
	}

	cwdToPIDs := make(map[string][]int)
	for _, e := range entries {
		cwdToPIDs[e.CWD] = append(cwdToPIDs[e.CWD], e.PID)
	}

	var results []model.AgentSession
	for cwd, pidsInCwd := range cwdToPIDs {
		matching := matchAllGeminiSessionsByCwd(cwd, sessions)

		// Sort PIDs ascending (lower PID = earlier process).
		sort.Ints(pidsInCwd)
		// Sort sessions by startTime ascending (earlier session first).
		sort.Slice(matching, func(i, j int) bool {
			return matching[i].Data.StartTime < matching[j].Data.StartTime
		})

		// Pair PIDs with sessions 1:1.
		for i, pid := range pidsInCwd {
			if i < len(matching) {
				sess := &matching[i]
				status := geminiStatusFromSession(&sess.Data)
				results = append(results, model.AgentSession{
					Agent:     "gemini",
					Status:    status,
					SessionID: sess.Data.SessionID,
					Directory: cwd,
					PID:       pid,
				})
			} else {
				// More PIDs than sessions — unknown status.
				results = append(results, model.AgentSession{
					Agent:     "gemini",
					Status:    model.StatusUnknown,
					Directory: cwd,
					PID:       pid,
				})
			}
		}
	}
	return results
}

// filterGeminiParents removes child processes from the PID list.
// A PID is a child if its PPID is also in the set (the parent node process).
func filterGeminiParents(pids []int) []int {
	pidSet := make(map[int]struct{}, len(pids))
	for _, pid := range pids {
		pidSet[pid] = struct{}{}
	}

	var parents []int
	for _, pid := range pids {
		ppid := platform.P.ReadProcessPPID(pid)
		if _, isChild := pidSet[ppid]; !isChild {
			parents = append(parents, pid)
		}
	}
	return parents
}

// findGeminiPIDs returns PIDs of Gemini CLI processes.
// Gemini runs as a Node.js program, so argv[0] is "node".
// We match any argument ending with /gemini or equal to "gemini".
func findGeminiPIDs() []int {
	re := regexp.MustCompile(`(^|/)gemini$`)
	return platform.P.FindPIDsByArgs(re)
}

// loadGeminiSessions scans ~/.gemini/tmp/*/chats/session-*.json and parses each file.
func loadGeminiSessions() []geminiSessionFile {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	tmpDir := filepath.Join(home, ".gemini", "tmp")
	projectDirs, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil
	}

	var sessions []geminiSessionFile
	for _, pd := range projectDirs {
		if !pd.IsDir() {
			continue
		}

		projectPath := filepath.Join(tmpDir, pd.Name())
		chatsDir := filepath.Join(projectPath, "chats")
		entries, err := os.ReadDir(chatsDir)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), "session-") || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}

			path := filepath.Join(chatsDir, e.Name())
			info, err := e.Info()
			if err != nil {
				continue
			}

			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}

			var session geminiSession
			if json.Unmarshal(data, &session) != nil {
				continue
			}

			sessions = append(sessions, geminiSessionFile{
				Path:       path,
				ModTime:    info.ModTime(),
				ProjectDir: projectPath,
				Data:       session,
			})
		}
	}

	return sessions
}

// matchAllGeminiSessionsByCwd returns all sessions whose project directory matches the CWD.
func matchAllGeminiSessionsByCwd(cwd string, sessions []geminiSessionFile) []geminiSessionFile {
	var matched []geminiSessionFile
	for i := range sessions {
		if matchGeminiProject(cwd, sessions[i].ProjectDir) {
			matched = append(matched, sessions[i])
		}
	}
	return matched
}

// matchGeminiProject checks if the CWD matches the project directory.
// It tries two approaches:
// 1. Read .project_root file in the project directory for the actual project path.
// 2. Compare the directory name directly with the CWD basename.
func matchGeminiProject(cwd, projectDir string) bool {
	// Approach 1: Check .project_root file.
	projectRootFile := filepath.Join(projectDir, ".project_root")
	data, err := os.ReadFile(projectRootFile)
	if err == nil {
		projectRoot := strings.TrimSpace(string(data))
		if projectRoot != "" {
			if cwd == projectRoot || strings.HasPrefix(cwd, projectRoot+"/") {
				return true
			}
		}
	}

	// Approach 2: Directory name matches CWD basename.
	dirName := filepath.Base(projectDir)
	cwdBase := filepath.Base(cwd)
	return dirName == cwdBase
}

// geminiStatusFromSession reads the last message's type to determine status.
//
// | Last message type | → Status |
// |-------------------|----------|
// | "user"            | BUSY     |
// | "gemini"          | IDLE     |
// | "error"           | IDLE     |
// | "info"            | IDLE     |
func geminiStatusFromSession(session *geminiSession) string {
	if len(session.Messages) == 0 {
		// No messages — session just started, waiting for user input.
		return model.StatusIdle
	}

	lastMsg := session.Messages[len(session.Messages)-1]
	switch lastMsg.Type {
	case "user":
		return model.StatusBusy
	default:
		// "gemini", "error", "info", or any other type → idle.
		return model.StatusIdle
	}
}
