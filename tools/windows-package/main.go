// Command windows-package appends the signed MSI before Authenticode signing.
package main

import (
	"flag"
	"fmt"
	"log"
	"shixin/internal/winsetup"
)

func main() {
	exe := flag.String("exe", "", "unsigned bootstrapper")
	msi := flag.String("msi", "", "signed MSI")
	ver := flag.String("version", "", "stable release version")
	arch := flag.String("arch", "", "amd64 or arm64")
	stampDir := flag.String("stamp-dir", "", "stamp owned Windows executable/DLL versions before signing")
	res := flag.String("version-res", "", "write ARM64X link-time version resource")
	skip := flag.String("skip", "", "already versioned ARM64X filename to leave intact")
	llamaDir := flag.String("check-llama", "", "verify native llama DLL architecture and self-contained imports")
	crtFiles := flag.Bool("crt-files", false, "list app-local Microsoft release runtime filenames")
	flag.Parse()
	if *crtFiles {
		for _, name := range microsoftCRTDLLs {
			fmt.Println(name)
		}
		return
	}
	if *llamaDir != "" {
		if err := checkLlama(*llamaDir, *arch); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *res != "" {
		if err := writeVersionRES(*res, *ver); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *stampDir != "" {
		if err := stamp(*stampDir, *ver, *skip); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := winsetup.Append(*exe, *msi, *ver, *arch); err != nil {
		log.Fatal(err)
	}
}
