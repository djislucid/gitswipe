package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

func getPath(bin, help string) string {
	binPath, err := exec.LookPath(bin)
	if err != nil {
		errorf := fmt.Errorf("%v : %s\n", err, help)
		fmt.Println(errorf.Error())
	}
	return binPath
}

func calculateConcurrencySize(taskSize int) int {
	if taskSize <= 100 {
		return taskSize / 2
	} else {
		return 50
	}
}

func printWantedFileContents(file string) {
	matched, _ := regexp.MatchString(`.*[.png|.jpg|.css|.ttf|.svg|.svgz|.woff|.gif|.git|.zip|.jar|.tar.gz|.db|._trace]$`, file)
	if matched {
		return
	}

	// make sure it's not a dir before printing
	f, err := os.Open(file)
	if err != nil {
		log.Fatal(err)
	}

	info, err := f.Stat()
	if !info.IsDir() {
		contents, err := ioutil.ReadFile(file)
		if err != nil {
			log.Fatal(err)
		}
		// pass to stdin some fucking how
		fmt.Println(string(contents))
	}

}

func readRepositoryFiles(reposDirectory string) {
	fileList := []string{}
	err := filepath.Walk(reposDirectory, func(path string, f os.FileInfo, err error) error {
		if !strings.Contains(path, "/.git/") {
			fileList = append(fileList, path)
		}
		return nil
	})

	for _, file := range fileList {
		printWantedFileContents(file)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func githubCloneRepo(repo, directory string, debug bool) {
	git := getPath("git", "You haven't installed git or it isn't in your path")
	cmd := exec.Command(git, "clone", repo, directory)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}

	cmd.Start()
	output := bufio.NewScanner(stdout)
	for output.Scan() {
		line := output.Text()
		if debug {
			fmt.Println(line)
		}
	}

	if err := cmd.Wait(); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Successfully cloned %s\n", repo)
}

func main() {
	var name string
	var debug, read, local bool
	flag.StringVar(&name, "n", "", "Specify the name of the organization or user")
	flag.BoolVar(&read, "r", false, "Read the contents of all files in the repositories")
	flag.BoolVar(&debug, "d", false, "Print the actual git clone output to stdout")
	flag.BoolVar(&local, "l", false, "Read the contents of a local directory instead of cloning from Github")
	flag.Parse()

	if !local {
		// create a new Github client
		ctx := context.Background()
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: os.Getenv("GITHUB_TOKEN")},
		)
		tc := oauth2.NewClient(ctx, ts)
		client := github.NewClient(tc)

		opt := &github.RepositoryListByOrgOptions{
			Type: "public",
			ListOptions: github.ListOptions{
				PerPage: 10,
			},
		}

		// collect all the repository URLs belonging to the organization
		var allRepos []*github.Repository
		for {
			repos, resp, err := client.Repositories.ListByOrg(ctx, name, opt)
			if err != nil {
				log.Fatal(err)
			}
			allRepos = append(allRepos, repos...)

			// pagination
			if resp.NextPage == 0 {
				break
			}
			opt.Page = resp.NextPage
		}

		// make a directory to store all the repos in
		if err := os.Mkdir(name, 0755); err != nil {
			log.Fatal(err)
		}

		// get location of the newly created directory
		path, err := filepath.Abs(fmt.Sprintf("./%s", name))
		if err != nil {
			log.Fatal(err)
		}

		// Worker pool to clone all repos faster
		var wg sync.WaitGroup
		var tasks = make(chan string)
		var workers = calculateConcurrencySize(len(allRepos))
		for i := 0; i < workers; i++ {
			wg.Add(1)

			// Each goroutine runs the modules function against a single task
			go func() {
				defer wg.Done()
				for repo := range tasks {
					dir := strings.Split(repo, "/")[4]
					clonePath := fmt.Sprintf("%s/%s", path, dir)
					githubCloneRepo(repo, clonePath, debug)
				}
			}()
		}
		// multithreading here
		for _, repos := range allRepos {
			tasks <- repos.GetHTMLURL()
		}

		go func() {
			close(tasks)
		}()
		wg.Wait()
	}

	// Ok. Now that all the repos are downloaded you can read the files
	if read {
		repoPath, err := filepath.Abs(name)
		if err != nil {
			log.Fatal(err)
		}
		readRepositoryFiles(repoPath)
	}

	// perfect. Now... just need to implement the damn ruby regex
}
