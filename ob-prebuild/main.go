package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	ugo "github.com/metaleap/go-util"
	uio "github.com/metaleap/go-util/io"
)

var (
	wait        sync.WaitGroup
	hiveDirPath = ugo.GopathSrcGithub("openbase", "ob-build", "hive-default")
)

func compilerRun(isBatch bool, cmd string, args ...string) {
	var (
		output []byte
		err    error
	)
	if isBatch && runtime.GOOS == "windows" {
		args = append([]string{"/C", cmd}, args...)
		cmd = "cmd"
	}
	if output, err = exec.Command(cmd, args...).CombinedOutput(); err != nil {
		log.Printf("[%s]\tERROR: %v\n", cmd, err)
	} else if len(output) > 0 {
		log.Println(string(output))
	}
}

func compileWebFiles() (err error) {
	prepDirPath := ugo.GopathSrcGithub("openbase", "ob-build", "hive-prep")
	prepTmpPath := filepath.Join(prepDirPath, "_tmp")
	if err = uio.EnsureDirExists(prepTmpPath); err != nil {
		return
	}

	//	convert hive-prep/path/file.old to hive-default/path/file.new
	getOutFilePath := func(srcFilePath, newExt string) (outFilePath string) {
		if srcDir, srcExt, srcBase := filepath.Dir(srcFilePath), filepath.Ext(srcFilePath), filepath.Base(srcFilePath); !strings.HasPrefix(srcBase, "_") {
			outFilePath = filepath.Join(hiveDirPath, srcDir[len(prepDirPath):], srcBase[:len(srcBase)-len(srcExt)]+newExt)
		}
		return
	}

	//	is file.src newer than file.dst?
	isNewer := func(srcFilePath, outFilePath string) (newer bool) {
		newer, _ = uio.IsNewerThan(srcFilePath, outFilePath)
		return
	}

	//	preprocess file.src -> file.dst
	prepFile := func(filePath string) {
		defer wait.Done()
		var outFilePath string
		switch filepath.Ext(filePath) {
		case ".scss":
			if outFilePath = getOutFilePath(filePath, ".css"); len(outFilePath) > 0 && isNewer(filePath, outFilePath) {
				compilerRun(true, "sass", "--trace", "--scss", "--stop-on-error", "-f", "-g", "-l", "-t", "expanded", "--cache-location", prepTmpPath, filePath, outFilePath)
			}
			if outFilePath = getOutFilePath(filePath, ".min.css"); len(outFilePath) > 0 && isNewer(filePath, outFilePath) {
				compilerRun(true, "sass", "--trace", "--scss", "--stop-on-error", "-f", "-t", "compressed", "--cache-location", prepTmpPath, filePath, outFilePath)
			}
		}
	}

	if errs := uio.NewDirWalker(true, nil, func(_ *uio.DirWalker, filePath string, _ os.FileInfo) bool {
		if !strings.HasPrefix(filePath, prepTmpPath) {
			wait.Add(1)
			go prepFile(filePath)
		}
		return true
	}).Walk(prepDirPath); len(errs) > 0 {
		err = errs[0]
	}
	return
}

func copyHive(dst string, cust bool) {
	defer wait.Done()
	subDirs := []string{"dist"}
	if cust {
		subDirs = append(subDirs, "cust")
	}
	for _, subDir := range subDirs {
		if err := uio.ClearDirectory(filepath.Join(dst, subDir)); err != nil {
			panic(err)
		}
		if err := uio.CopyAll(filepath.Join(hiveDirPath, subDir), filepath.Join(dst, subDir), nil); err != nil {
			panic(err)
		}
	}
}

func resetCust() {
	distDirPath, custDirPath := filepath.Join(hiveDirPath, "dist"), filepath.Join(hiveDirPath, "cust")
	//	clear everything in cust
	uio.ClearDirectory(custDirPath)
	//	recreate entire dist directory hierarchy in cust, but empty (no files, only directories)
	uio.NewDirWalker(true, func(_ *uio.DirWalker, dirPath string, _ os.FileInfo) bool {
		uio.EnsureDirExists(filepath.Join(custDirPath, dirPath[len(distDirPath):]))
		return true
	}, nil).Walk(distDirPath)
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	resetCust()
	err := compileWebFiles()
	if err != nil {
		panic(err)
	}
	wait.Wait()

	//	copy to GAE demo-app/hive
	wait.Add(1)
	go copyHive(ugo.GopathSrcGithub("openbase", "ob-gae", "demo-app", "hive"), true)

	//	copy to other user-specified hive directory such as "/user/foo/my-ob-dev/test2"
	dst := flag.String("hive_dst", "", fmt.Sprintf("Destination hive dir path to copy %s to", hiveDirPath))
	cust := flag.Bool("hive_cust", true, fmt.Sprintf("Set to true to copy %s/cust to {hive_dst}/cust", hiveDirPath))
	if flag.Parse(); len(*dst) > 0 {
		wait.Add(1)
		go copyHive(*dst, *cust)
	}

	wait.Wait()
}
