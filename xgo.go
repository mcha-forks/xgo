// Go CGO cross compiler
// Copyright (c) 2014 Péter Szilágyi. All rights reserved.
//
// Released under the MIT license.

// Wrapper around the GCO cross compiler docker container.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/build"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Cross compilation docker containers
var dockerBase = "karalabe/xgo-base"
var dockerDist = "karalabe/xgo-"

// Command line arguments to fine tune the compilation
var (
	goVersion   = flag.String("go", "latest", "Go release to use for cross compilation")
	inPackage   = flag.String("pkg", "", "Sub-package to build if not root import")
	outPrefix   = flag.String("out", "", "Prefix to use for output naming (empty = package name)")
	srcRemote   = flag.String("remote", "", "Version control remote repository to build")
	srcBranch   = flag.String("branch", "", "Version control branch to build")
	crossDeps   = flag.String("deps", "", "CGO dependencies (configure/make based archives)")
	targets     = flag.String("targets", "*/*", "Comma separated targets to build for")
	dockerImage = flag.String("image", "", "Use custom docker image instead of official distribution")
)

// Command line arguments to pass to go build
var buildVerbose = flag.Bool("v", false, "Print the names of packages as they are compiled")
var buildSteps = flag.Bool("x", false, "Print the command as executing the builds")
var buildRace = flag.Bool("race", false, "Enable data race detection (supported only on amd64)")

func main() {
	flag.Parse()

	// Ensure docker is available
	if err := checkDocker(); err != nil {
		log.Fatalf("Failed to check docker installation: %v.", err)
	}
	// Validate the command line arguments
	if len(flag.Args()) != 1 {
		log.Fatalf("Usage: %s [options] <go import path>", os.Args[0])
	}
	// Select the image to use, either official or custom
	image := dockerDist + *goVersion
	if *dockerImage != "" {
		image = *dockerImage
	}
	// Check that all required images are available
	found, err := checkDockerImage(image)
	switch {
	case err != nil:
		log.Fatalf("Failed to check docker image availability: %v.", err)
	case !found:
		fmt.Println("not found!")
		if err := pullDockerImage(image); err != nil {
			log.Fatalf("Failed to pull docker image from the registry: %v.", err)
		}
	default:
		fmt.Println("found.")
	}
	// Cross compile the requested package into the local folder
	if err := compile(flag.Args()[0], image, *srcRemote, *srcBranch, *inPackage, *crossDeps, *outPrefix, *buildVerbose, *buildSteps, *buildRace, strings.Split(*targets, ",")); err != nil {
		log.Fatalf("Failed to cross compile package: %v.", err)
	}
}

// Checks whether a docker installation can be found and is functional.
func checkDocker() error {
	fmt.Println("Checking docker installation...")
	if err := run(exec.Command("docker", "version")); err != nil {
		return err
	}
	fmt.Println()
	return nil
}

// Checks whether a required docker image is available locally.
func checkDockerImage(image string) (bool, error) {
	fmt.Printf("Checking for required docker image %s... ", image)
	out, err := exec.Command("docker", "images", "--no-trunc").Output()
	if err != nil {
		return false, err
	}
	return bytes.Contains(out, []byte(image)), nil
}

// Pulls an image from the docker registry.
func pullDockerImage(image string) error {
	fmt.Printf("Pulling %s from docker registry...\n", image)
	return run(exec.Command("docker", "pull", image))
}

// Cross compiles a requested package into the current working directory.
func compile(repo string, image string, remote string, branch string, pack string, deps string, prefix string, verbose bool, steps bool, race bool, targets []string) error {
	// Retrieve the current folder to store the binaries in
	folder, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to retrieve the working directory: %v.", err)
	}
	// If a local build was requested, find the import path and mount all GOPATH sources
	locals, mounts, paths := []string{}, []string{}, []string{}
	if strings.HasPrefix(repo, string(filepath.Separator)) || strings.HasPrefix(repo, ".") {
		// Resolve the repository import path from the file path
		path, err := filepath.Abs(repo)
		if err != nil {
			log.Fatalf("Failed to locate requested package: %v.", err)
		}
		stat, err := os.Stat(path)
		if err != nil || !stat.IsDir() {
			log.Fatalf("Requested path invalid.")
		}
		pack, err := build.ImportDir(path, build.FindOnly)
		if err != nil {
			log.Fatalf("Failed to resolve import path: %v.", err)
		}
		repo = pack.ImportPath

		// Iterate over all the local libs and export the mount points
		for i, gopath := range strings.Split(os.Getenv("GOPATH"), string(os.PathListSeparator)) {
			locals = append(locals, filepath.Join(gopath, "src"))
			mounts = append(mounts, filepath.Join("/ext-go", strconv.Itoa(i), "src"))
			paths = append(paths, filepath.Join("/ext-go", strconv.Itoa(i)))
		}
	}
	// Assemble and run the cross compilation command
	fmt.Printf("Cross compiling %s...\n", repo)

	args := []string{
		"run",
		"-v", folder + ":/build",
		"-e", "REPO_REMOTE=" + remote,
		"-e", "REPO_BRANCH=" + branch,
		"-e", "PACK=" + pack,
		"-e", "DEPS=" + deps,
		"-e", "OUT=" + prefix,
		"-e", fmt.Sprintf("FLAG_V=%v", verbose),
		"-e", fmt.Sprintf("FLAG_X=%v", steps),
		"-e", fmt.Sprintf("FLAG_RACE=%v", race),
		"-e", "TARGETS=" + strings.Replace(strings.Join(targets, " "), "*", ".", -1),
	}
	for i := 0; i < len(locals); i++ {
		args = append(args, []string{"-v", fmt.Sprintf("%s:%s:ro", locals[i], mounts[i])}...)
	}
	args = append(args, []string{"-e", "EXT_GOPATH=" + strings.Join(paths, ":")}...)

	args = append(args, []string{image, repo}...)
	return run(exec.Command("docker", args...))
}

// Executes a command synchronously, redirecting its output to stdout.
func run(cmd *exec.Cmd) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
