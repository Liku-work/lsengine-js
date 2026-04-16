// internal/imports/import_processor.go
package imports

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"lsengine/internal/metrics"
	"lsengine/internal/security"
)

type ImportProcessor struct {
	securityProcessor *security.ImportProcessor
}

func NewImportProcessor(projectRoot string) *ImportProcessor {
	return &ImportProcessor{
		securityProcessor: security.NewImportProcessor(projectRoot),
	}
}

func (ip *ImportProcessor) ProcessImports(htmlContent string) string {
	scriptRe := regexp.MustCompile(`(?s)<script\b(?:[^>]*)>\s*((?:[\s\S]*?import\s+\{[^}]+\}\s+from\s+['"][^'"]+['"][\s\S]*?))\s*</script>`)

	importCounter := 0

	return scriptRe.ReplaceAllStringFunc(htmlContent, func(match string) string {
		submatches := scriptRe.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}

		importCounter++
		if importCounter > security.GlobalSecurityPolicy.MaxImportsPerFile {
			metrics.IncSecurityBlocked("too_many_imports")
			log.Printf("[Import][BLOCKED] Límite de %d imports por archivo superado", security.GlobalSecurityPolicy.MaxImportsPerFile)
			return "<!-- BLOQUEADO: demasiados imports en este archivo -->"
		}

		scriptContent := submatches[1]
		return ip.processScriptBlock(scriptContent)
	})
}

func (ip *ImportProcessor) processScriptBlock(scriptContent string) string {
	importLineRe := regexp.MustCompile(`(?m)^[ \t]*import\s+\{([^}]+)\}\s+from\s+['"]([^'"]+)['"]\s*;?[ \t]*$`)

	var injectedParts []string
	var codeLines []string

	lines := strings.Split(scriptContent, "\n")
	ctx := security.NewImportContext()

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if matches := importLineRe.FindStringSubmatch(trimmed); len(matches) >= 3 {
			importsRaw := matches[1]
			sourcePath := strings.TrimSpace(matches[2])

			// Usar el método auxiliar para incrementar el contador de forma segura
			count := ctx.IncrementCount()

			if count > security.GlobalSecurityPolicy.MaxImportsPerFile {
				injectedParts = append(injectedParts, fmt.Sprintf("// BLOQUEADO: import #%d excede el límite", count))
				continue
			}

			lower := strings.ToLower(sourcePath)
			if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "//") {
				metrics.IncSecurityBlocked("remote_import_attempt")
				log.Printf("[Import][BLOCKED] Remote import blocked: %s", sourcePath)
				injectedParts = append(injectedParts,
					fmt.Sprintf("// BLOCKED FOR SECURITY: remote import not allowed (%s)", sourcePath))
				continue
			}

			importSpecifiers := parseImportSpecifiers(importsRaw)
			code, err := ip.resolveImport(sourcePath, importSpecifiers, ctx)
			if err != nil {
				log.Printf("[Import][ERROR] %s: %v", sourcePath, err)
				injectedParts = append(injectedParts,
					fmt.Sprintf("// ERROR loading import '%s': %v", sourcePath, err))
				continue
			}
			injectedParts = append(injectedParts, code)
		} else {
			codeLines = append(codeLines, line)
		}
	}

	var sb strings.Builder
	sb.WriteString("<script>\n")

	for _, part := range injectedParts {
		sb.WriteString(part)
		sb.WriteString("\n")
	}

	userCode := strings.TrimSpace(strings.Join(codeLines, "\n"))
	if userCode != "" {
		sb.WriteString("// --- código del bloque script ---\n")
		sb.WriteString("(function() {\n")
		sb.WriteString(userCode)
		sb.WriteString("\n})();\n")
	}

	sb.WriteString("</script>")
	return sb.String()
}

func parseImportSpecifiers(raw string) []string {
	raw = strings.TrimSpace(raw)
	var result []string
	for _, part := range strings.Split(raw, ",") {
		s := strings.TrimSpace(part)
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

func (ip *ImportProcessor) resolveImport(sourcePath string, specifiers []string, ctx *security.ImportContext) (string, error) {
	if ctx.GetDepth() >= security.GlobalSecurityPolicy.MaxImportDepth {
		return "", fmt.Errorf("profundidad máxima de imports alcanzada (%d)", security.GlobalSecurityPolicy.MaxImportDepth)
	}

	cacheKey := sourcePath
	if ctx.CheckAndMarkVisited(cacheKey) {
		return fmt.Sprintf("// (ya importado: %s)", sourcePath), nil
	}

	ctx.IncrementDepth()
	defer ctx.DecrementDepth()

	content, err := ip.securityProcessor.ReadLocalFile(sourcePath)
	if err != nil {
		return "", err
	}

	if err := security.ValidateScriptContent(content); err != nil {
		return "", fmt.Errorf("contenido rechazado de %s: %v", sourcePath, err)
	}

	isAll := false
	for _, s := range specifiers {
		if s == "all" || s == "*" {
			isAll = true
			break
		}
	}

	if isAll {
		return fmt.Sprintf(
			"// === import {all} from \"%s\" ===\n%s\n// === fin import {all} ===",
			sourcePath, content,
		), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("// === import {%s} from \"%s\" ===\n", strings.Join(specifiers, ", "), sourcePath))
	sb.WriteString("(function() {\n")
	sb.WriteString("  var module = { exports: {} };\n")
	sb.WriteString("  var exports = module.exports;\n")
	sb.WriteString(content)
	sb.WriteString("\n")

	for _, spec := range specifiers {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf(
			"  if (typeof module.exports['%s'] !== 'undefined') { window['%s'] = module.exports['%s']; }\n"+
				"  else if (typeof exports['%s'] !== 'undefined') { window['%s'] = exports['%s']; }\n"+
				"  else if (typeof %s !== 'undefined') { window['%s'] = %s; }\n",
			spec, spec, spec,
			spec, spec, spec,
			spec, spec, spec,
		))
	}

	sb.WriteString("})();\n")
	sb.WriteString(fmt.Sprintf("// === fin import {%s} ===", strings.Join(specifiers, ", ")))

	return sb.String(), nil
}

func ProcessToCallTags(content, projectRoot string) string {
	re := regexp.MustCompile(`<script\s+to-call=["']([^"']+)["']\s*(?:[^>]*)?>\s*</script>`)
	result := re.ReplaceAllStringFunc(content, func(match string) string {
		submatches := re.FindStringSubmatch(match)
		if len(submatches) < 2 {
			return match
		}
		filePath := submatches[1]

		lower := strings.ToLower(filePath)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "//") {
			metrics.IncSecurityBlocked("remote_to_call")
			return fmt.Sprintf("<!-- BLOQUEADO: to-call remoto no permitido (%s) -->", filePath)
		}

		fullPath, err := security.ResolveLocalPath(projectRoot, filePath)
		if err != nil {
			return fmt.Sprintf("<!-- Error: %v -->", err)
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Sprintf("<!-- Error: no se pudo cargar %s -->", filePath)
		}

		if err := security.ValidateScriptContent(string(data)); err != nil {
			metrics.IncSecurityBlocked("blocked_content_to_call")
			return fmt.Sprintf("<!-- BLOQUEADO: contenido peligroso en %s -->", filePath)
		}

		return fmt.Sprintf("<script>\n// Contenido incluido desde %s\n%s\n</script>", filePath, string(data))
	})
	return result
}

func ProcessScriptSrcLS(htmlContent, projectRoot string) string {
	re := regexp.MustCompile(`(?i)<script\s+([^>]*?)src=["']([^"']+\.js)["']([^>]*?)(ls-ws|ls)\s*(?:[^>]*?)>\s*</script>`)

	return re.ReplaceAllStringFunc(htmlContent, func(match string) string {
		submatches := re.FindStringSubmatch(match)
		if len(submatches) < 5 {
			return match
		}

		filePath := submatches[2]
		variant := strings.ToLower(submatches[4])

		lower := strings.ToLower(filePath)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "//") {
			metrics.IncSecurityBlocked("remote_script_src_ls")
			return fmt.Sprintf("<!-- BLOQUEADO: script-ls remoto no permitido (%s) -->", filePath)
		}

		fullPath, err := security.ResolveLocalPath(projectRoot, filePath)
		if err != nil {
			return fmt.Sprintf("<!-- Error cargando ls-script %s: %v -->", filePath, err)
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Sprintf("<!-- Error leyendo ls-script %s -->", filePath)
		}

		jsContent := string(data)

		if err := security.ValidateScriptContent(jsContent); err != nil {
			metrics.IncSecurityBlocked("script_content_blocked")
			return fmt.Sprintf("<!-- BLOQUEADO: contenido peligroso en %s -->", filePath)
		}

		if variant == "ls-ws" {
			return fmt.Sprintf(`<script>
// ls-ws: %s
(function() {
  function _runWhenReady(fn) {
    if (typeof ls !== 'undefined' && typeof ls.fs !== 'undefined') {
      fn();
    } else {
      var _check = setInterval(function() {
        if (typeof ls !== 'undefined' && typeof ls.fs !== 'undefined') {
          clearInterval(_check);
          fn();
        }
      }, 50);
    }
  }
  _runWhenReady(function() {
%s
  });
})();
</script>`, filePath, jsContent)
		}

		return fmt.Sprintf(`<script>
// ls-script: %s
%s
</script>`, filePath, jsContent)
	})
}