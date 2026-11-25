package config

import (
	"os"
	"path"
	"regexp"
	"strings"
)

// ChangedImages returns the images whose Dockerfile folder has changes among
// files (slash-separated, relative to the working directory, as git reports
// them), each with the file that changed it. A changed file belongs to its
// closest Dockerfile folder: its own folder, or the nearest one above it that
// holds the Dockerfile of a configured image. Folders above that one are not
// affected. In that folder:
//
//   - a changed Dockerfile changes the images built from it
//   - with a single Dockerfile, any other changed file changes its images
//   - with several Dockerfiles, another changed file changes the images whose
//     Dockerfile mentions it: its path relative to the folder (conf/app.yaml)
//     or one of its parent folders (conf/ or conf)
//
// Children are not included: they follow their parent through the image tree
// (the version cascade, the graph's dependents for --changed-only).
func (c *Config) ChangedImages(files []string) map[*ImageConfig]string {
	byDockerfile := map[string][]*ImageConfig{} // Dockerfile -> images built from it
	folders := map[string][]string{}            // folder -> its Dockerfiles
	for _, img := range c.allImages() {
		df := cleanPath(img.GetDockerfilePath(c.Config.Dockerfiles))
		if len(byDockerfile[df]) == 0 {
			folders[path.Dir(df)] = append(folders[path.Dir(df)], df)
		}
		byDockerfile[df] = append(byDockerfile[df], img)
	}

	changed := map[*ImageConfig]string{}
	mark := func(df, file string) {
		for _, img := range byDockerfile[df] {
			if _, ok := changed[img]; !ok {
				changed[img] = file
			}
		}
	}
	contents := map[string]string{}
	read := func(df string) string {
		if _, ok := contents[df]; !ok {
			data, _ := os.ReadFile(df) // a missing Dockerfile mentions nothing
			contents[df] = string(data)
		}
		return contents[df]
	}

	for _, f := range files {
		f = cleanPath(f)
		folder, ok := dockerfileFolder(f, folders)
		if !ok {
			continue
		}
		dockerfiles := folders[folder]
		switch {
		case len(byDockerfile[f]) > 0:
			mark(f, f)
		case len(dockerfiles) == 1:
			mark(dockerfiles[0], f)
		default:
			rel := strings.TrimPrefix(f, folder+"/")
			if folder == "." {
				rel = f
			}
			for _, df := range dockerfiles {
				if mentions(read(df), rel) {
					mark(df, f)
				}
			}
		}
	}
	return changed
}

// dockerfileFolder returns the closest folder holding Dockerfiles that
// contains file: its own folder or the nearest one above it.
func dockerfileFolder(file string, folders map[string][]string) (string, bool) {
	for dir := path.Dir(file); ; dir = path.Dir(dir) {
		if _, ok := folders[dir]; ok {
			return dir, true
		}
		if dir == "." || dir == "/" {
			return "", false
		}
	}
}

// mentions reports whether a Dockerfile's text names rel (a path relative to
// its folder) or one of rel's parent folders, as a whole path: conf/app.yaml is
// mentioned by "COPY conf/app.yaml ..." or "COPY conf/ ...", not by
// "COPY myconf/ ..." or "COPY conf/other.yaml ...".
func mentions(dockerfile, rel string) bool {
	parts := strings.Split(rel, "/")
	for n := len(parts); n > 0; n-- {
		p := regexp.QuoteMeta(strings.Join(parts[:n], "/"))
		if regexp.MustCompile(`(^|[\s"'=,\[]|\./)` + p + `/?([\s"',\]]|$)`).MatchString(dockerfile) {
			return true
		}
	}
	return false
}
