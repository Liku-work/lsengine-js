// internal/security/security.go
package security

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"lsengine/internal/metrics"
)

const (
	MAX_IMPORT_FILE_SIZE = 1 * 1024 * 1024
	MAX_IMPORTS_PER_FILE = 50
	MAX_IMPORT_DEPTH     = 5
	MAX_SCRIPT_CODE_SIZE = 512 * 1024
)

type SecurityPolicy struct {
	AllowRemoteImports bool
	AllowedExtensions  map[string]bool
	AllowedSubdirs     []string
	MaxFileSize        int64
	MaxImportsPerFile  int
	MaxImportDepth     int
	BlockedPatterns    []*regexp.Regexp
}

var GlobalSecurityPolicy = &SecurityPolicy{
	AllowRemoteImports: false,
	AllowedExtensions: map[string]bool{
		".js":   true,
		".mjs":  true,
		".json": true,
	},
	AllowedSubdirs: []string{
		"js", "public", "public/js", "modules", "scripts", "src", "lib", "components",
	},
	MaxFileSize:       MAX_IMPORT_FILE_SIZE,
	MaxImportsPerFile: MAX_IMPORTS_PER_FILE,
	MaxImportDepth:    MAX_IMPORT_DEPTH,
	BlockedPatterns: []*regexp.Regexp{
		regexp.MustCompile(`(?i)eval\s*\(`),
		regexp.MustCompile(`(?i)document\.write\s*\(`),
		regexp.MustCompile(`setTimeout\s*\(\s*['"][^'"]*['"]\s*,\s*\d+\s*\)`),
		regexp.MustCompile(`setInterval\s*\(\s*['"][^'"]*['"]\s*,\s*\d+\s*\)`),
	},
}

type ImportContext struct {
	Depth   int
	Visited map[string]bool
	Count   int
	mu      sync.Mutex
}

func NewImportContext() *ImportContext {
	return &ImportContext{
		Depth:   0,
		Visited: make(map[string]bool),
		Count:   0,
	}
}

func (ic *ImportContext) Lock() {
	ic.mu.Lock()
}

func (ic *ImportContext) Unlock() {
	ic.mu.Unlock()
}

func (ic *ImportContext) IncrementCount() int {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	ic.Count++
	return ic.Count
}

func (ic *ImportContext) CheckAndMarkVisited(key string) bool {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	if ic.Visited[key] {
		return true
	}
	ic.Visited[key] = true
	return false
}

func (ic *ImportContext) IncrementDepth() {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	ic.Depth++
}

func (ic *ImportContext) DecrementDepth() {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	ic.Depth--
}

func (ic *ImportContext) GetDepth() int {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	return ic.Depth
}

type ImportProcessor struct {
	ProjectRoot string
	ImportCache map[string]string
	mu          sync.RWMutex
	client      interface{}
}

func NewImportProcessor(root string) *ImportProcessor {
	return &ImportProcessor{
		ProjectRoot: root,
		ImportCache: make(map[string]string),
		client:      nil,
	}
}

func (ip *ImportProcessor) ValidateLocalPath(rawPath string) (string, error) {
	lower := strings.ToLower(strings.TrimSpace(rawPath))
	if strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "//") ||
		strings.HasPrefix(lower, "ftp://") ||
		strings.HasPrefix(lower, "data:") ||
		strings.HasPrefix(lower, "blob:") {
		metrics.IncSecurityBlocked("remote_import_attempt")
		return "", fmt.Errorf("imports remotos bloqueados por política de seguridad: %s", rawPath)
	}

	cleanPath := filepath.Clean(rawPath)
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	cleanPath = strings.TrimPrefix(cleanPath, "\\")

	fullPath := filepath.Join(ip.ProjectRoot, cleanPath)

	rel, err := filepath.Rel(ip.ProjectRoot, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, "/") {
		metrics.IncSecurityBlocked("path_traversal")
		return "", fmt.Errorf("acceso denegado: ruta fuera del proyecto: %s", rawPath)
	}

	ext := strings.ToLower(filepath.Ext(fullPath))
	if !GlobalSecurityPolicy.AllowedExtensions[ext] {
		metrics.IncSecurityBlocked("forbidden_extension")
		return "", fmt.Errorf("extensión no permitida: %s", ext)
	}

	allowed := false
	if !strings.Contains(rel, string(filepath.Separator)) {
		allowed = true
	} else {
		firstDir := strings.Split(rel, string(filepath.Separator))[0]
		for _, sub := range GlobalSecurityPolicy.AllowedSubdirs {
			if firstDir == sub || strings.HasPrefix(rel, sub+string(filepath.Separator)) {
				allowed = true
				break
			}
		}
	}

	if !allowed {
		metrics.IncSecurityBlocked("forbidden_directory")
		return "", fmt.Errorf("directorio no permitido para imports: %s (relativo: %s)", rawPath, rel)
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		if !strings.HasSuffix(fullPath, ".js") {
			jsPath := fullPath + ".js"
			info2, err2 := os.Stat(jsPath)
			if err2 == nil && !info2.IsDir() {
				fullPath = jsPath
				info = info2
				goto sizeCheck
			}
		}
		return "", fmt.Errorf("archivo no encontrado: %s", rawPath)
	}

	if info.IsDir() {
		for _, candidate := range []string{"main.js", "index.js"} {
			candidatePath := filepath.Join(fullPath, candidate)
			if ci, err := os.Stat(candidatePath); err == nil && !ci.IsDir() {
				info = ci
				fullPath = candidatePath
				goto sizeCheck
			}
		}
		return "", fmt.Errorf("directorio sin punto de entrada: %s", rawPath)
	}

sizeCheck:
	if info.Size() > GlobalSecurityPolicy.MaxFileSize {
		metrics.IncSecurityBlocked("file_too_large")
		return "", fmt.Errorf("archivo demasiado grande: %d > %d bytes", info.Size(), GlobalSecurityPolicy.MaxFileSize)
	}

	return fullPath, nil
}

func ValidateScriptContent(content string) error {
	for _, pattern := range GlobalSecurityPolicy.BlockedPatterns {
		if pattern.MatchString(content) {
			metrics.IncSecurityBlocked("blocked_pattern")
			return fmt.Errorf("contenido bloqueado: patrón peligroso detectado (%s)", pattern.String())
		}
	}
	if len(content) > MAX_SCRIPT_CODE_SIZE {
		metrics.IncSecurityBlocked("script_too_large")
		return fmt.Errorf("script demasiado grande: %d bytes", len(content))
	}
	return nil
}

func (ip *ImportProcessor) ReadLocalFile(rawPath string) (string, error) {
	ip.mu.RLock()
	if cached, ok := ip.ImportCache[rawPath]; ok {
		ip.mu.RUnlock()
		return cached, nil
	}
	ip.mu.RUnlock()

	fullPath, err := ip.ValidateLocalPath(rawPath)
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("error leyendo archivo: %v", err)
	}

	content := string(data)

	ip.mu.Lock()
	ip.ImportCache[rawPath] = content
	ip.mu.Unlock()

	return content, nil
}

func ResolveLocalPath(projectRoot, path string) (string, error) {
	lower := strings.ToLower(strings.TrimSpace(path))
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "//") {
		return "", fmt.Errorf("URLs remotas no permitidas: %s", path)
	}

	cleanPath := filepath.Clean(path)
	cleanPath = strings.TrimPrefix(cleanPath, "/")
	cleanPath = strings.TrimPrefix(cleanPath, "\\")

	fullPath := filepath.Join(projectRoot, cleanPath)
	rel, err := filepath.Rel(projectRoot, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("acceso denegado: %s", path)
	}

	ext := strings.ToLower(filepath.Ext(fullPath))
	if ext != "" && !GlobalSecurityPolicy.AllowedExtensions[ext] {
		return "", fmt.Errorf("extensión no permitida: %s", ext)
	}

	if _, err := os.Stat(fullPath); err == nil {
		return fullPath, nil
	}
	if !strings.HasSuffix(fullPath, ".js") {
		jsPath := fullPath + ".js"
		if _, err := os.Stat(jsPath); err == nil {
			return jsPath, nil
		}
	}
	for _, subdir := range []string{"js", "public/js", "modules"} {
		candidate := filepath.Join(projectRoot, subdir, cleanPath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("archivo no encontrado: %s", path)
}