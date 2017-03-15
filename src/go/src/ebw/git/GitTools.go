package git

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang/glog"
	"github.com/google/go-github/github"

	"ebw/config"
	"ebw/util"
)

var ErrNoGitDirectory = errors.New(`Not a GIT directory`)

// ErrUnknownUser indicates that Github was unable to resolve the user's name.
var ErrUnknownUser = errors.New(`UnknownUser: no recognized login or ID`)

// Username returns the username of the currently logged in user on GitHub.
func Username(cntxt context.Context, client *github.Client) (string, error) {
	// Empty username gives the currently logged-in user
	user, _, err := client.Users.Get(cntxt, "")
	if nil != err {
		return ``, util.Error(err)
	}
	if nil != user.Login {
		return *user.Login, nil
	}
	if nil != user.ID {
		return strconv.FormatInt(int64(*user.ID), 10), nil
	}
	return ``, ErrUnknownUser
}

// RepoDir returns the local git_cache repo location.
func RepoDir(user, repo string) (string, error) {
	root, err := os.Getwd()
	if nil != err {
		return ``, util.Error(err)
	}
	root = filepath.Join(root, config.Config.GitCache, `repos`, user)
	if `` == repo {
		return root, nil
	}
	return filepath.Join(root, repo), nil
}

// runGit runs git in the user/repo directory with the given args, returning error
// on failure.
func runGit(user, repo string, args []string) error {
	root, err := RepoDir(user, repo)
	if nil != err {
		return err
	}
	return runGitDir(root, args)
}

func runGitDir(dir string, args []string) error {
	glog.Infof(`git command dir=%s, args = [%v]`, dir, args)
	cmd := exec.Command(`git`, args...)
	cmd.Dir = dir

	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// getGitOutput runs the git command in the given directory
// and returns stdout as a string
func getGitOutput(dir string, args []string) (string, error) {
	if `` == dir {
		var err error
		dir, err = os.Getwd()
		if nil != err {
			return ``, util.Error(err)
		}
	}
	glog.Infof(`git command dir=%s, args = [%v]`, dir, args)
	cmd := exec.Command(`git`, args...)
	cmd.Dir = dir

	var stdOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdOut, os.Stderr

	if err := cmd.Run(); nil != err {
		return ``, util.Error(err)
	}
	return stdOut.String(), nil
}

// Checkout checks out the github repo into the cached directory system,
// and returns the path to the root of the repo. If the client is already
// checked out, it updates from the origin server.
func Checkout(client *github.Client, user, name, url string) (string, error) {
	if `` == url {
		url = fmt.Sprintf(`https://github.com/%s/%s`, user, name)
	}
	glog.Infof(`Cloning/updating %s/%s from %s`, user, name, url)
	root, err := RepoDir(user, ``)
	if nil != err {
		return ``, util.Error(err)
	}
	os.MkdirAll(root, 0755)
	_, err = os.Stat(filepath.Join(root, name))
	if nil == err {
		return gitUpdate(client, filepath.Join(root, name))
	}
	if !os.IsNotExist(err) {
		return ``, util.Error(err)
	}

	cmd := exec.Command(`git`, `clone`, url+`.git`)
	cmd.Dir = root

	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return filepath.Join(root, name), cmd.Run()
}

// gitUpdate updates the files in the given repo root directory.
func gitUpdate(client *github.Client, root string) (string, error) {
	cmd := exec.Command(`git`, `pull`, `origin`, `master`)
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	glog.Infof("dir = %s: git pull origin master", root)
	return root, cmd.Run()
}

// RemoteName returns a name for the remote based on the remoteUrl
func RemoteName(remoteUrl string) string {
	return fmt.Sprintf(`%x`, md5.Sum([]byte(remoteUrl)))
}

// RemoteAdd adds a new Remote to the git remotes
func RemoteAdd(client *github.Client, user, repo, remoteUrl string) (string, error) {
	remote := RemoteName(remoteUrl)
	return remote, runGit(user, repo, []string{`remote`, `add`, remote, remoteUrl})
}

// UrlUserRepo returns the user and repo given a github URL
func UrlUserRepo(remoteUrl string) (string, string, error) {
	reg := regexp.MustCompile(`github.com/([^/]+)/([^/]+)`)
	if m := reg.FindStringSubmatch(remoteUrl); nil != m {
		return m[1], m[2], nil
	}
	return ``, ``, fmt.Errorf(`repo %s is not a github repo`, remoteUrl)
}

// PullRequestVersions returns the local and remote version of the file named in filePath.
func PullRequestVersions(cntxt context.Context, client *github.Client, user, repo, remoteUrl, remoteSha, filePath string) (string, string, error) {
	// We are sure this exists because of the point at which we call
	// this from the JS front-end

	// prRoot, err := PullRequestCheckout(remoteUrl, remoteSha)
	// if nil != err {
	// 	return ``, ``, err
	// }
	prRoot, err := PullRequestDir(remoteSha)
	if nil != err {
		return ``, ``, err
	}

	repoDir, err := RepoDir(user, repo)
	if nil != err {
		return ``, ``, err
	}

	localFileRaw, err := ioutil.ReadFile(filepath.Join(repoDir, filePath))
	if nil != err {
		if !os.IsNotExist(err) {
			return ``, ``, util.Error(err)
		}
		localFileRaw = []byte{}
	}

	remoteFileRaw, err := ioutil.ReadFile(filepath.Join(prRoot, filePath))
	if nil != err {
		if !os.IsNotExist(err) {
			return ``, ``, util.Error(err)
		}
		remoteFileRaw = []byte{}
	}

	return string(localFileRaw), string(remoteFileRaw), nil
}

// DuplicateRepo duplicates the template repo into the user's github repos, and gives it
// the name newRepo
// This is used to start a new book, without being a fork of the EBW electric-book repo.
// See https://help.github.com/articles/duplicating-a-repository/ for more infromation.
func DuplicateRepo(cntxt context.Context, client *github.Client, githubPassword string, templateRepo string, newRepo string) error {
	repoName := filepath.Base(newRepo)
	// 1. Check the user doesn't already have a newRepo, and if not, create
	// a newRepo for the user
	user, _, err := client.Users.Get(cntxt, "") // Get the current authenticated user
	if nil != err {
		return util.Error(err)
	}
	githubUsername := *user.Login

	workingDir := filepath.Join(os.TempDir(), githubUsername, newRepo)
	os.MkdirAll(workingDir, 0755)

	_, _, err = client.Repositories.Create(cntxt, "", &github.Repository{
		Name:  &newRepo,
		Owner: user,
	})
	if nil != err {
		return util.Error(err)
	}

	// 2. Checkout the templateRepo with --bare into a new directory called [repoName]
	if err := runGitDir(workingDir, []string{
		`clone`,
		`--bare`,
		`https://github.com/` + templateRepo + `.git`,
		repoName,
	}); nil != err {
		return util.Error(err)
	}

	// 3. Mirror-push to the newRepo
	if err := runGitDir(filepath.Join(workingDir, repoName), []string{
		`push`, `--mirror`, `https://` + githubUsername + `:` + githubPassword + `@github.com/` + *user.Login + `/` + repoName + `.git`,
	}); nil != err {
		return util.Error(err)
	}
	// 4. Delete the temporary working directory
	if err := os.RemoveAll(filepath.Join(workingDir, repoName)); nil != err {
		return util.Error(err)
	}
	return nil
}

func ContributeToRepo(cntxt context.Context, client *github.Client) error {
	//func DuplicateRepo(cntxt context.Context, client *github.Client, githubPassword string, newRepo string) error {
	return nil
}

func GitCloneTo(cntxt context.Context, client *github.Client, workingDir,
	githubPassword string, repoUsername, repoName string) error {
	if "" == workingDir {
		wd, err := os.Getwd()
		if nil != err {
			return util.Error(err)
		}
		workingDir = wd
	}
	// 1. Check the user doesn't already have a newRepo, and if not, create
	// a newRepo for the user
	user, _, err := client.Users.Get(cntxt, "") // Get the current authenticated user
	if nil != err {
		return util.Error(err)
	}
	githubUsername := *user.Login
	if "" == repoUsername {
		repoUsername = githubUsername
	}

	if err := runGitDir(workingDir, []string{
		`clone`,
		`https://` + githubUsername + ":" + githubPassword +
			"@github.com/" + repoUsername + "/" + repoName + ".git",
	}); nil != err {
		return util.Error(err)
	}
	return nil
}

// GithubDeleteRepo deletes a repository on the
// github systems.
// See https://developer.github.com/v3/repos/#delete-a-repository
func GithubDeleteRepo(apiToken string, githubUsername string,
	repoName string) error {
	requestUrl := `https://api.github.com/repos/` + githubUsername + `/` + repoName
	req, err := http.NewRequest(`DELETE`,
		requestUrl, nil)
	if nil != err {
		return util.Error(err)
	}
	req.Header.Add(`Authorization`, `token `+apiToken)
	client := &http.Client{}
	res, err := client.Do(req)
	if nil != err {
		return util.Error(err)
	}
	defer res.Body.Close()
	if 200 > res.StatusCode || 300 <= res.StatusCode {
		fmt.Printf(`Command is: 
curl -v -X DELETE -H "Authorization: token %s" '%s'
`, apiToken, requestUrl)
		return fmt.Errorf(`Bad status code result: %d (ensure your token has repo_delete privileges)`, res.StatusCode)
	}
	io.Copy(os.Stdout, res.Body)
	return nil

}

// GitRemoteRepo returns the remote repo name of the given remote
// It expects a remote of the form:
// [remotename] [remoteURL] ([fetch|push)])
// so parses the results of `git remote get-url [remote]`
// which is expected to be a URL, takes the path and strips .git
func GitRemoteRepo(cntxt context.Context, workingDir, remote string) (remoteUser, remoteProject string, err error) {
	if `` == remote {
		remote = `origin`
	}
	if `` == workingDir {
		workingDir, err = os.Getwd()
		if nil != err {
			return ``, ``, util.Error(err)
		}
	}

	remoteUrl, err := getGitOutput(workingDir, []string{
		`remote`,
		`get-url`,
		remote,
	})
	if nil != err {
		return ``, ``, err
	}
	ru, err := url.Parse(remoteUrl)
	if nil != err {
		return ``, ``, util.Error(err)
	}
	path := strings.TrimPrefix(strings.TrimSpace(ru.Path), `/`)
	if strings.HasSuffix(path, `.git`) {
		path = path[0 : len(path)-4]
	}
	paths := strings.Split(path, `/`)
	remoteProject = paths[len(paths)-1]
	remoteUser = strings.Join(paths[0:len(paths)-1], `/`)

	return remoteUser, remoteProject, nil
}

// GitCurrentBranch returns the name of the currently checkedout
// branch.
func GitCurrentBranch(cntxt context.Context, workingDir string) (string, error) {
	branchesOut, err := getGitOutput(workingDir, []string{
		`branch`, `--list`,
	})
	if nil != err {
		return ``, err
	}
	branches := strings.Split(branchesOut, "\n")
	for _, b := range branches {
		if 0 == len(b) {
			continue
		}
		// Current branch indicated with an asterisk
		if b[0] == '*' {
			return strings.TrimSpace(b[1:]), nil
		}
	}
	return ``, errors.New(`No current branch`)
}

// GitFindRepoRootDirectory returns the first parent directory containing
// a .git subfolder, or an error if no such directory is found.
func GitFindRepoRootDirectory(workingDir string) (string, error) {
	var err error
	if `` == workingDir {
		workingDir, err = os.Getwd()
		if nil != err {
			return ``, util.Error(err)
		}
	}
	_, err = os.Stat(filepath.Join(workingDir, `.git`))
	if nil == err {
		return workingDir, nil
	}
	if !os.IsNotExist(err) {
		return ``, util.Error(err)
	}
	// .git directory doesn't exist in this directory
	parent := filepath.Dir(workingDir)
	if 0 == len(parent) || workingDir == parent {
		return ``, ErrNoGitDirectory
	}
	return GitFindRepoRootDirectory(parent)
}