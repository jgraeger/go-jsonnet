/*
Copyright 2017 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/fatih/color"

	"github.com/jgraeger/go-jsonnet"
	"github.com/jgraeger/go-jsonnet/cmd/internal/cmd"
)

func version(o io.Writer) {
	fmt.Fprintf(o, "Jsonnet commandline interpreter (Go implementation) %s\n", jsonnet.Version())
}

func usage(o io.Writer) {
	version(o)
	fmt.Fprintln(o)
	fmt.Fprintln(o, "jsonnet {<option>} <filename>")
	fmt.Fprintln(o)
	fmt.Fprintln(o, "Available options:")
	fmt.Fprintln(o, "  -h / --help                This message")
	fmt.Fprintln(o, "  -e / --exec                Treat filename as code")
	fmt.Fprintln(o, "  -J / --jpath <dir>         Specify an additional library search dir")
	fmt.Fprintln(o, "                             (right-most wins)")
	fmt.Fprintln(o, "  -o / --output-file <file>  Write to the output file rather than stdout")
	fmt.Fprintln(o, "  -m / --multi <dir>         Write multiple files to the directory, list files")
	fmt.Fprintln(o, "                             on stdout")
	fmt.Fprintln(o, "  -c / --create-output-dirs  Automatically creates all parent directories for")
	fmt.Fprintln(o, "                             files")
	fmt.Fprintln(o, "  -y / --yaml-stream         Write output as a YAML stream of JSON documents")
	fmt.Fprintln(o, "  -S / --string              Expect a string, manifest as plain text")
	fmt.Fprintln(o, "  -s / --max-stack <n>       Number of allowed stack frames")
	fmt.Fprintln(o, "  -t / --max-trace <n>       Max length of stack trace before cropping")
	fmt.Fprintln(o, "  --version                  Print version")
	fmt.Fprintln(o)
	fmt.Fprintln(o, "Available options for specifying values of 'external' variables:")
	fmt.Fprintln(o, "  Provide the value as a string:")
	fmt.Fprintln(o, "  -V / --ext-str <var>[=<val>]      If <val> is omitted, get from environment")
	fmt.Fprintln(o, "                                    var <var>")
	fmt.Fprintln(o, "       --ext-str-file <var>=<file>  Read the string from the file")
	fmt.Fprintln(o, "  Provide a value as Jsonnet code:")
	fmt.Fprintln(o, "  --ext-code <var>[=<code>]         If <code> is omitted, get from environment")
	fmt.Fprintln(o, "                                    var <var>")
	fmt.Fprintln(o, "  --ext-code-file <var>=<file>      Read the code from the file")
	fmt.Fprintln(o)
	fmt.Fprintln(o, "Available options for specifying values of 'top-level arguments':")
	fmt.Fprintln(o, "  Provide the value as a string:")
	fmt.Fprintln(o, "  -A / --tla-str <var>[=<val>]      If <val> is omitted, get from environment")
	fmt.Fprintln(o, "                                    var <var>")
	fmt.Fprintln(o, "       --tla-str-file <var>=<file>  Read the string from the file")
	fmt.Fprintln(o, "  Provide a value as Jsonnet code:")
	fmt.Fprintln(o, "  --tla-code <var>[=<code>]         If <code> is omitted, get from environment")
	fmt.Fprintln(o, "                                    var <var>")
	fmt.Fprintln(o, "  --tla-code-file <var>=<file>      Read the code from the file")
	fmt.Fprintln(o)
	fmt.Fprintln(o, "Environment variables:")
	fmt.Fprintln(o, "  JSONNET_PATH is a colon (semicolon on Windows) separated list of directories")
	fmt.Fprintln(o, "  added in reverse order before the paths specified by --jpath (i.e. left-most")
	fmt.Fprintln(o, "  wins). E.g. these are equivalent:")
	fmt.Fprintln(o, "    JSONNET_PATH=a:b jsonnet -J c -J d")
	fmt.Fprintln(o, "    JSONNET_PATH=d:c:a:b jsonnet")
	fmt.Fprintln(o, "    jsonnet -J b -J a -J c -J d")
	fmt.Fprintln(o)
	fmt.Fprintln(o, "In all cases:")
	fmt.Fprintln(o, "  <filename> can be - (stdin)")
	fmt.Fprintln(o, "  Multichar options are expanded e.g. -abc becomes -a -b -c.")
	fmt.Fprintln(o, "  The -- option suppresses option processing for subsequent arguments.")
	fmt.Fprintln(o, "  Note that since filenames and jsonnet programs can begin with -, it is")
	fmt.Fprintln(o, "  advised to use -- if the argument is unknown, e.g. jsonnet -- \"$FILENAME\".")
}

type config struct {
	outputFile           string
	evalMultiOutputDir   string
	inputFiles           []string
	evalJpath            []string
	filenameIsCode       bool
	evalMulti            bool
	evalStream           bool
	evalCreateOutputDirs bool
}

func makeConfig() config {
	return config{
		filenameIsCode: false,
		evalMulti:      false,
		evalStream:     false,
		evalJpath:      []string{},
	}
}

func getVarVal(s string) (string, string, error) {
	parts := strings.SplitN(s, "=", 2)
	name := parts[0]
	if len(parts) == 1 {
		content, exists := os.LookupEnv(name)
		if exists {
			return name, content, nil
		}
		return "", "", fmt.Errorf("environment variable %v was undefined", name)
	}
	return name, parts[1], nil
}

func getVarFile(s string, imp string) (string, string, error) {
	parts := strings.SplitN(s, "=", 2)
	name := parts[0]
	if len(parts) == 1 {
		return "", "", fmt.Errorf(`argument not in form <var>=<file> "%s"`, s)
	}
	return name, fmt.Sprintf("%s @'%s'", imp, strings.Replace(parts[1], "'", "''", -1)), nil
}

type processArgsStatus int

const (
	processArgsStatusContinue     = iota
	processArgsStatusSuccessUsage = iota
	processArgsStatusFailureUsage = iota
	processArgsStatusSuccess      = iota
	processArgsStatusFailure      = iota
)

func processArgs(givenArgs []string, config *config, vm *jsonnet.VM) (processArgsStatus, error) {
	args := cmd.SimplifyArgs(givenArgs)
	remainingArgs := make([]string, 0, len(args))
	i := 0

	handleVarVal := func(handle func(key string, val string)) error {
		next := cmd.NextArg(&i, args)
		name, content, err := getVarVal(next)
		if err != nil {
			return err
		}
		handle(name, content)
		return nil
	}

	handleVarFile := func(handle func(key string, val string), imp string) error {
		next := cmd.NextArg(&i, args)
		name, content, err := getVarFile(next, imp)
		if err != nil {
			return err
		}
		handle(name, content)
		return nil
	}

	for ; i < len(args); i++ {
		arg := args[i]
		if arg == "-h" || arg == "--help" {
			return processArgsStatusSuccessUsage, nil
		} else if arg == "-v" || arg == "--version" {
			version(os.Stdout)
			return processArgsStatusSuccess, nil
		} else if arg == "-e" || arg == "--exec" {
			config.filenameIsCode = true
		} else if arg == "-o" || arg == "--output-file" {
			outputFile := cmd.NextArg(&i, args)
			if len(outputFile) == 0 {
				return processArgsStatusFailure, fmt.Errorf("-o argument was empty string")
			}
			config.outputFile = outputFile
		} else if arg == "--" {
			// All subsequent args are not options.
			i++
			for ; i < len(args); i++ {
				remainingArgs = append(remainingArgs, args[i])
			}
			break
		} else if arg == "-s" || arg == "--max-stack" {
			l := cmd.SafeStrToInt(cmd.NextArg(&i, args))
			if l < 1 {
				return processArgsStatusFailure, fmt.Errorf("invalid --max-stack value: %d", l)
			}
			vm.MaxStack = l
		} else if arg == "-J" || arg == "--jpath" {
			dir := cmd.NextArg(&i, args)
			if len(dir) == 0 {
				return processArgsStatusFailure, fmt.Errorf("-J argument was empty string")
			}
			config.evalJpath = append(config.evalJpath, dir)
		} else if arg == "-V" || arg == "--ext-str" {
			if err := handleVarVal(vm.ExtVar); err != nil {
				return processArgsStatusFailure, err
			}
		} else if arg == "--ext-str-file" {
			if err := handleVarFile(vm.ExtCode, "importstr"); err != nil {
				return processArgsStatusFailure, err
			}
		} else if arg == "--ext-code" {
			if err := handleVarVal(vm.ExtCode); err != nil {
				return processArgsStatusFailure, err
			}
		} else if arg == "--ext-code-file" {
			if err := handleVarFile(vm.ExtCode, "import"); err != nil {
				return processArgsStatusFailure, err
			}
		} else if arg == "-A" || arg == "--tla-str" {
			if err := handleVarVal(vm.TLAVar); err != nil {
				return processArgsStatusFailure, err
			}
		} else if arg == "--tla-str-file" {
			if err := handleVarFile(vm.TLACode, "importstr"); err != nil {
				return processArgsStatusFailure, err
			}
		} else if arg == "--tla-code" {
			if err := handleVarVal(vm.TLACode); err != nil {
				return processArgsStatusFailure, err
			}
		} else if arg == "--tla-code-file" {
			if err := handleVarFile(vm.TLACode, "import"); err != nil {
				return processArgsStatusFailure, err
			}
		} else if arg == "-t" || arg == "--max-trace" {
			l := cmd.SafeStrToInt(cmd.NextArg(&i, args))
			if l < 0 {
				return processArgsStatusFailure, fmt.Errorf("invalid --max-trace value: %d", l)
			}
			vm.ErrorFormatter.SetMaxStackTraceSize(l)
		} else if arg == "-m" || arg == "--multi" {
			config.evalMulti = true
			outputDir := cmd.NextArg(&i, args)
			if len(outputDir) == 0 {
				return processArgsStatusFailure, fmt.Errorf("-m argument was empty string")
			}
			if outputDir[len(outputDir)-1] != '/' {
				outputDir += "/"
			}
			config.evalMultiOutputDir = outputDir
		} else if arg == "-c" || arg == "--create-output-dirs" {
			config.evalCreateOutputDirs = true
		} else if arg == "-y" || arg == "--yaml-stream" {
			config.evalStream = true
		} else if arg == "-S" || arg == "--string" {
			vm.StringOutput = true
		} else if len(arg) > 1 && arg[0] == '-' {
			return processArgsStatusFailure, fmt.Errorf("unrecognized argument: %s", arg)
		} else {
			remainingArgs = append(remainingArgs, arg)
		}
	}

	want := "filename"
	if config.filenameIsCode {
		want = "code"
	}
	if len(remainingArgs) == 0 {
		return processArgsStatusFailureUsage, fmt.Errorf("must give %s", want)
	}

	// TODO(dcunnin): Formatter allows multiple files in test and in-place mode.
	multipleFilesAllowed := false

	if !multipleFilesAllowed {
		if len(remainingArgs) > 1 {
			return processArgsStatusFailure, fmt.Errorf("only one %s is allowed", want)
		}
	}

	config.inputFiles = remainingArgs
	return processArgsStatusContinue, nil
}

func writeMultiOutputFiles(output map[string]string, outputDir, outputFile string, createDirs bool) (err error) {
	// If multiple file output is used, then iterate over each string from
	// the sequence of strings returned by jsonnet_evaluate_snippet_multi,
	// construct pairs of filename and content, and write each output file.

	var manifest *os.File

	if outputFile == "" {
		manifest = os.Stdout
	} else {
		manifest, err = os.Create(outputFile)
		if err != nil {
			return err
		}
		defer func() {
			if ferr := manifest.Close(); ferr != nil {
				err = ferr
			}
		}()
	}

	// Iterate through the map in order.
	keys := make([]string, 0, len(output))
	for k := range output {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		newContent := output[key]
		filename := outputDir + key

		_, err := manifest.WriteString(filename)
		if err != nil {
			return err
		}

		_, err = manifest.WriteString("\n")
		if err != nil {
			return err
		}

		if _, err := os.Stat(filename); !os.IsNotExist(err) {
			existingContent, err := ioutil.ReadFile(filename)
			if err != nil {
				return err
			}
			if string(existingContent) == newContent {
				// Do not bump the timestamp on the file if its content is
				// the same. This may trigger other tools (e.g. make) to do
				// unnecessary work.
				continue
			}
		}
		if createDirs {
			if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
				return err
			}
		}

		err = ioutil.WriteFile(filename, []byte(newContent), 0666)
		if err != nil {
			return err
		}
	}

	return nil
}

// writeOutputStream writes the output as a YAML stream.
func writeOutputStream(output []string, outputFile string) (err error) {
	var f *os.File

	if outputFile == "" {
		f = os.Stdout
	} else {
		f, err = os.Create(outputFile)
		if err != nil {
			return err
		}
		defer func() {
			if ferr := f.Close(); ferr != nil {
				err = ferr
			}
		}()
	}

	for _, doc := range output {
		_, err := f.WriteString("---\n")
		if err != nil {
			return err
		}
		_, err = f.WriteString(doc)
		if err != nil {
			return err
		}
	}

	if len(output) > 0 {
		_, err := f.WriteString("...\n")
		if err != nil {
			return err
		}
	}

	return nil
}

func main() {
	cmd.StartCPUProfile()
	defer cmd.StopCPUProfile()

	vm := jsonnet.MakeVM()
	vm.ErrorFormatter.SetColorFormatter(color.New(color.FgRed).Fprintf)

	config := makeConfig()
	jsonnetPath := filepath.SplitList(os.Getenv("JSONNET_PATH"))
	for i := len(jsonnetPath) - 1; i >= 0; i-- {
		config.evalJpath = append(config.evalJpath, jsonnetPath[i])
	}

	status, err := processArgs(os.Args[1:], &config, vm)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: "+err.Error())
	}
	switch status {
	case processArgsStatusContinue:
		break
	case processArgsStatusSuccessUsage:
		usage(os.Stdout)
		os.Exit(0)
	case processArgsStatusFailureUsage:
		if err != nil {
			fmt.Fprintln(os.Stderr, "")
		}
		usage(os.Stderr)
		os.Exit(1)
	case processArgsStatusSuccess:
		os.Exit(0)
	case processArgsStatusFailure:
		os.Exit(1)
	}

	vm.Importer(&jsonnet.FileImporter{
		JPaths: config.evalJpath,
	})

	if len(config.inputFiles) != 1 {
		// Should already have been caught by processArgs.
		panic("Internal error: expected a single input file.")
	}
	filename := config.inputFiles[0]
	// TODO(sbarzowski) Clean up SafeReadInput to be more in line with the new API
	input := cmd.SafeReadInput(config.filenameIsCode, &filename)
	var output string
	var outputArray []string
	var outputDict map[string]string
	if config.filenameIsCode || config.inputFiles[0] == "-" {
		if config.evalMulti {
			outputDict, err = vm.EvaluateAnonymousSnippetMulti(filename, input)
		} else if config.evalStream {
			outputArray, err = vm.EvaluateAnonymousSnippetStream(filename, input)
		} else {
			output, err = vm.EvaluateAnonymousSnippet(filename, input)
		}
	} else {
		if config.evalMulti {
			outputDict, err = vm.EvaluateFileMulti(filename)
		} else if config.evalStream {
			outputArray, err = vm.EvaluateFileStream(filename)
		} else {
			output, err = vm.EvaluateFile(filename)
		}
	}

	cmd.MemProfile()

	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	// Write output JSON.
	if config.evalMulti {
		err := writeMultiOutputFiles(outputDict, config.evalMultiOutputDir, config.outputFile, config.evalCreateOutputDirs)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	} else if config.evalStream {
		err := writeOutputStream(outputArray, config.outputFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	} else {
		err := cmd.WriteOutputFile(output, config.outputFile, config.evalCreateOutputDirs)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
	}

}
