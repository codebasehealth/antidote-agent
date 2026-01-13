package discovery

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/codebasehealth/antidote-agent/internal/messages"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"gopkg.in/yaml.v3"
)

// Discover gathers information about the server
func Discover() *messages.DiscoveryMessage {
	msg := messages.NewDiscoveryMessage()

	// Basic info
	msg.Hostname, _ = os.Hostname()
	msg.OS = runtime.GOOS
	msg.Arch = runtime.GOARCH

	// Host info
	if info, err := host.Info(); err == nil {
		msg.Distro = info.Platform + " " + info.PlatformVersion
		msg.Kernel = info.KernelVersion
		msg.Uptime = int64(info.Uptime)
	}

	// System info
	msg.System = gatherSystemInfo()

	// Services
	msg.Services = discoverServices()

	// Languages
	msg.Languages = discoverLanguages()

	// Apps
	msg.Apps = discoverApps()

	// Docker
	msg.Docker = discoverDocker()

	return msg
}

func gatherSystemInfo() messages.SystemInfo {
	info := messages.SystemInfo{}

	info.CPUCores = runtime.NumCPU()

	if mem, err := mem.VirtualMemory(); err == nil {
		info.MemoryTotal = mem.Total
		info.MemoryFree = mem.Available
	}

	if disk, err := disk.Usage("/"); err == nil {
		info.DiskTotal = disk.Total
		info.DiskFree = disk.Free
	}

	if avg, err := load.Avg(); err == nil {
		info.LoadAvg = avg.Load1
	}

	return info
}

func discoverServices() []messages.ServiceInfo {
	services := []messages.ServiceInfo{}

	// Common services to check
	serviceNames := []string{
		"nginx",
		"apache2",
		"httpd",
		"mysql",
		"mariadb",
		"postgresql",
		"redis",
		"redis-server",
		"memcached",
		"php-fpm",
		"php8.3-fpm",
		"php8.2-fpm",
		"php8.1-fpm",
		"php8.0-fpm",
		"supervisor",
		"supervisord",
	}

	for _, name := range serviceNames {
		if status := checkServiceStatus(name); status != "" {
			svc := messages.ServiceInfo{
				Name:   name,
				Status: status,
			}
			// Try to get version
			svc.Version = getServiceVersion(name)
			services = append(services, svc)
		}
	}

	return services
}

func checkServiceStatus(name string) string {
	// Try systemctl first
	cmd := exec.Command("systemctl", "is-active", name)
	out, err := cmd.Output()
	if err == nil {
		status := strings.TrimSpace(string(out))
		if status == "active" {
			return "running"
		}
		return status
	}

	// Try service command
	cmd = exec.Command("service", name, "status")
	if err := cmd.Run(); err == nil {
		return "running"
	}

	return ""
}

func getServiceVersion(name string) string {
	var cmd *exec.Cmd

	switch {
	case strings.HasPrefix(name, "php"):
		cmd = exec.Command("php", "-v")
	case name == "nginx":
		cmd = exec.Command("nginx", "-v")
	case name == "mysql" || name == "mariadb":
		cmd = exec.Command("mysql", "--version")
	case name == "postgresql":
		cmd = exec.Command("psql", "--version")
	case name == "redis" || name == "redis-server":
		cmd = exec.Command("redis-server", "--version")
	default:
		return ""
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}

	// Extract version number
	re := regexp.MustCompile(`[\d]+\.[\d]+\.?[\d]*`)
	if match := re.FindString(string(out)); match != "" {
		return match
	}

	return ""
}

func discoverLanguages() []messages.LanguageInfo {
	languages := []messages.LanguageInfo{}

	// PHP
	if path, err := exec.LookPath("php"); err == nil {
		if out, err := exec.Command("php", "-v").Output(); err == nil {
			re := regexp.MustCompile(`PHP ([\d]+\.[\d]+\.[\d]+)`)
			if match := re.FindStringSubmatch(string(out)); len(match) > 1 {
				languages = append(languages, messages.LanguageInfo{
					Name:    "php",
					Version: match[1],
					Path:    path,
				})
			}
		}
	}

	// Node
	if path, err := exec.LookPath("node"); err == nil {
		if out, err := exec.Command("node", "-v").Output(); err == nil {
			version := strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
			languages = append(languages, messages.LanguageInfo{
				Name:    "node",
				Version: version,
				Path:    path,
			})
		}
	}

	// Python
	for _, pyCmd := range []string{"python3", "python"} {
		if path, err := exec.LookPath(pyCmd); err == nil {
			if out, err := exec.Command(pyCmd, "--version").Output(); err == nil {
				re := regexp.MustCompile(`Python ([\d]+\.[\d]+\.[\d]+)`)
				if match := re.FindStringSubmatch(string(out)); len(match) > 1 {
					languages = append(languages, messages.LanguageInfo{
						Name:    "python",
						Version: match[1],
						Path:    path,
					})
					break
				}
			}
		}
	}

	// Ruby
	if path, err := exec.LookPath("ruby"); err == nil {
		if out, err := exec.Command("ruby", "-v").Output(); err == nil {
			re := regexp.MustCompile(`ruby ([\d]+\.[\d]+\.[\d]+)`)
			if match := re.FindStringSubmatch(string(out)); len(match) > 1 {
				languages = append(languages, messages.LanguageInfo{
					Name:    "ruby",
					Version: match[1],
					Path:    path,
				})
			}
		}
	}

	// Go
	if path, err := exec.LookPath("go"); err == nil {
		if out, err := exec.Command("go", "version").Output(); err == nil {
			re := regexp.MustCompile(`go([\d]+\.[\d]+\.?[\d]*)`)
			if match := re.FindStringSubmatch(string(out)); len(match) > 1 {
				languages = append(languages, messages.LanguageInfo{
					Name:    "go",
					Version: match[1],
					Path:    path,
				})
			}
		}
	}

	return languages
}

func discoverApps() []messages.AppInfo {
	apps := []messages.AppInfo{}

	// Common app directories to check
	searchPaths := []string{
		"/home/forge",
		"/home/deploy",
		"/var/www",
		"/srv",
		"/app",
		"/opt/apps",
	}

	for _, basePath := range searchPaths {
		if _, err := os.Stat(basePath); os.IsNotExist(err) {
			continue
		}

		entries, err := os.ReadDir(basePath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			projectDir := filepath.Join(basePath, entry.Name())

			// Check for Forge/Capistrano-style deployment (with 'current' symlink)
			currentPath := filepath.Join(projectDir, "current")
			if info, err := os.Stat(currentPath); err == nil && info.IsDir() {
				// Use the 'current' directory as the app path
				if app := analyzeApp(currentPath); app != nil {
					apps = append(apps, *app)
				}
				continue
			}

			// Otherwise check the directory itself
			if app := analyzeApp(projectDir); app != nil {
				apps = append(apps, *app)
			}
		}
	}

	return apps
}

func analyzeApp(path string) *messages.AppInfo {
	app := &messages.AppInfo{
		Path: path,
	}

	// Check for antidote.yml first - this takes priority
	configPath := filepath.Join(path, "antidote.yml")
	if config := readAntidoteConfig(configPath); config != nil {
		app.Config = config
		app.Framework = config.App.Framework
	} else {
		// Auto-detect framework if no config
		if _, err := os.Stat(filepath.Join(path, "artisan")); err == nil {
			app.Framework = "laravel"
		} else if _, err := os.Stat(filepath.Join(path, "package.json")); err == nil {
			// Check for specific frameworks
			if _, err := os.Stat(filepath.Join(path, "next.config.js")); err == nil {
				app.Framework = "nextjs"
			} else if _, err := os.Stat(filepath.Join(path, "next.config.mjs")); err == nil {
				app.Framework = "nextjs"
			} else if _, err := os.Stat(filepath.Join(path, "next.config.ts")); err == nil {
				app.Framework = "nextjs"
			} else if _, err := os.Stat(filepath.Join(path, "nuxt.config.js")); err == nil {
				app.Framework = "nuxt"
			} else if _, err := os.Stat(filepath.Join(path, "nuxt.config.ts")); err == nil {
				app.Framework = "nuxt"
			} else {
				app.Framework = "node"
			}
		} else if _, err := os.Stat(filepath.Join(path, "Gemfile")); err == nil {
			app.Framework = "rails"
		} else if _, err := os.Stat(filepath.Join(path, "manage.py")); err == nil {
			app.Framework = "django"
		} else if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
			app.Framework = "go"
		} else {
			// Not a recognized app and no antidote.yml
			return nil
		}
	}

	// Git info
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		app.GitRemote = getGitRemote(path)
		app.GitBranch = getGitBranch(path)
		app.GitCommit = getGitCommit(path)
	}

	return app
}

// readAntidoteConfig reads and parses an antidote.yml file
func readAntidoteConfig(path string) *messages.AppConfig {
	log.Printf("Checking for config at: %s", path)

	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("No config file at %s: %v", path, err)
		return nil
	}

	log.Printf("Found config file at %s (%d bytes)", path, len(data))

	var config messages.AppConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		log.Printf("Failed to parse config at %s: %v", path, err)
		return nil
	}

	// Validate minimum required fields
	if config.App.Name == "" || config.App.Framework == "" {
		log.Printf("Config at %s missing required fields (name=%q, framework=%q)", path, config.App.Name, config.App.Framework)
		return nil
	}

	log.Printf("Successfully loaded config for %s (%s)", config.App.Name, config.App.Framework)
	return &config
}

func getGitRemote(path string) string {
	cmd := exec.Command("git", "-C", path, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func getGitBranch(path string) string {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func getGitCommit(path string) string {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func discoverDocker() *messages.DockerInfo {
	// Check if docker is available
	if _, err := exec.LookPath("docker"); err != nil {
		return nil
	}

	docker := &messages.DockerInfo{}

	// Get version
	if out, err := exec.Command("docker", "--version").Output(); err == nil {
		re := regexp.MustCompile(`Docker version ([\d]+\.[\d]+\.[\d]+)`)
		if match := re.FindStringSubmatch(string(out)); len(match) > 1 {
			docker.Version = match[1]
		}
	}

	// Get containers
	cmd := exec.Command("docker", "ps", "--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}")
	out, err := cmd.Output()
	if err != nil {
		return docker
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 4 {
			docker.Containers = append(docker.Containers, messages.ContainerInfo{
				ID:     parts[0],
				Name:   parts[1],
				Image:  parts[2],
				Status: parts[3],
			})
		}
	}

	return docker
}
