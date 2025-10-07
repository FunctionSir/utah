/*
 * @Author: FunctionSir
 * @License: AGPLv3
 * @Date: 2025-10-04 20:46:11
 * @LastEditTime: 2025-10-07 21:41:39
 * @LastEditors: FunctionSir
 * @Description: -
 * @FilePath: /utah/main.go
 */

package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	Version string = "0.1.0"
)

const (
	TapeSafetyMargin       int64 = 18 * 1024 * 1024 * 1024
	PerFileSafetyMarginMiB int64 = 1
)

const (
	CatalogTimeFormat string = "2006-01-02 15:04:05 MST"
)

var (
	ManifestPath    string = "" // A JSON file contains all meta of archives.
	Site            string = "main"
	Format          string = ".tar"          // Archive file type.
	Shell           string = "/usr/bin/bash" // Shell you wanted.
	TapeID          string = ""              // Usually barcode of cur tape.
	TapeCapAvailMiB int64  = -1              // Available capacity of cur tape as MiB.
)

type Manifest struct {
	ManifestName string    `json:"manifest_name"`
	UTAHVersion  string    `json:"utah_version"`
	CreatedAt    time.Time `json:"created_at"`
	LastModified time.Time `json:"last_modified"`
	Maintainer   string    `json:"maintainer"`
	Note         string    `json:"note"`
	Records      []Record  `json:"records"`
}

type Record struct {
	Name            string            `json:"name"`   // Example: "Miku Expo Collection 2025"
	Format          string            `json:"format"` // Example: ".tar.zstd.age"
	CreatedAt       time.Time         `json:"created_at"`
	Location        ArchiveLocation   `json:"location"`
	Size            int64             `json:"size"`     // As bytes.
	Checksum        string            `json:"checksum"` // SHA256 checksum.
	ExtraAttributes map[string]string `json:"extra_attributes"`
	Tags            []string          `json:"tags"`
	Notes           []string          `json:"notes"`
}

type ArchiveLocation struct {
	Site      string `json:"site"`       // Where you store tapes.
	TapeID    string `json:"tape_id"`    // Usually barcode.
	FileIndex int    `json:"file_index"` // File 0, 1, 2, ...
}

func Check(err error) {
	if err != nil {
		panic(err)
	}
}

func GotoShell() {
	cmd := exec.Command(Shell)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		fmt.Println(err.Error())
	}
}

func PromptAndReadLine(format string, a ...any) string {
	var input string
	for {
		fmt.Printf(format, a...)
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			panic(err)
		}
		input = strings.TrimSpace(line)
		if input == "$" {
			GotoShell()
		} else {
			break
		}
	}
	return input
}

func FileOrDirExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func IsAFile(path string) bool {
	stat, err := os.Stat(path)
	if os.IsNotExist(err) || stat.IsDir() {
		return false
	}
	return true
}

func ReadNotEmpty(format string, a ...any) string {
	var input string
	for input == "" {
		input = PromptAndReadLine(format, a...)
		if input == "" {
			fmt.Println("Input can not be empty, try again.")
		}
	}
	return input
}

func GetCommand() string {
	userInput := PromptAndReadLine("Your choice: ")
	return strings.ToUpper(strings.TrimSpace(userInput))
}

func Save(manifest *Manifest) {
	manifest.LastModified = time.Now()
	fmt.Println("Saving manifest file to disk...")
	f, err := os.Create(ManifestPath)
	Check(err)
	defer func() { _ = f.Close() }()
	data, err := json.MarshalIndent(manifest, "", "    ")
	Check(err)
	n, err := f.Write(data)
	Check(err)
	fmt.Printf("Written %d bytes of data.\n", n)
}

func Preferences() {
	userInput := PromptAndReadLine("Site (empty = \"%s\"): ", Site)
	if userInput != "" {
		Site = userInput
	}
	for {
		userInput := PromptAndReadLine("Format (a.k.a. file extension, empty = \"%s\"): ", Format)
		if userInput != "" {
			if userInput[0] != '.' {
				fmt.Println("Must be started with a dot, please try again.")
				continue
			}
			Format = userInput
		}
		break
	}
	for {
		userInput := PromptAndReadLine("Shell you want (empty = \"%s\"): ", Shell)
		if userInput == "" {
			userInput = Shell
		}
		if !IsAFile(userInput) {
			fmt.Println("No such file or is a dir, try again.")
			continue
		}
		Shell = userInput
		break
	}
	fmt.Println("All preferences set.")
}

func Add(manifest *Manifest) {
	fmt.Println("Hint: enter ! only to cancel and drop current record in most cases.")
	fmt.Println("Hint: \"! cancel\" is useless when entering tags, extra attributes or notes.")
	for TapeID == "" {
		TapeID = ReadNotEmpty("Input the ID (usually barcode) of target tape (scanner is recommended): ")
		fmt.Println("Hint: use \"sudo sg_logs -p 0x31 /dev/(n)stX\" to view remaining capacity in MiB.")
		tmp := PromptAndReadLine("Input the remaining capacity of the tape (in MiB): ")
		if tmp == "!" {
			fmt.Println("User dropped this record.")
			return
		}
		tmpNum, err := strconv.Atoi(tmp)
		if err != nil || tmpNum < 0 {
			fmt.Println("Invalid capacity, please try again.")
			continue
		}
		TapeCapAvailMiB = int64(tmpNum)
	}
	record := Record{CreatedAt: time.Now()}
	record.Name = ReadNotEmpty("Archive name: ")
	if record.Name == "!" {
		fmt.Println("User dropped this record.")
		return
	}
	record.Format = Format
	record.ExtraAttributes = make(map[string]string)
	record.Tags = make([]string, 0)
	record.Notes = make([]string, 0)
	fmt.Println("Hint: to make a tarball: tar -cvf [OUTNAME].tar [SRC1] [SRC2]...")
	fmt.Println("Hint: to make a compressed tarball: tar -cavf [OUTNAME].[EXT] [SRC1] [SRC2]...")
	fmt.Println("Hint: to make a level 19 zstd compressed tarball: tar --use-compress-program='zstd -T0 -19' -cvf [OUTNAME].tar.zst [SRC1] [SRC2]")
	fmt.Println("Hint: use tar with age encryption: tar -cavf - [SRC1] [SRC2]... | age -p > [OUTFILE].[EXT].age")
	fmt.Println("Now, you can gen the archive file using terminal below:")
	GotoShell()
	archivePath := ReadNotEmpty("Created archive file: ")
	if archivePath == "!" {
		fmt.Println("User dropped this record.")
		return
	}
	for !strings.HasSuffix(archivePath, Format) {
		fmt.Println("Format mismatch, might be a mistake, use terminal below to fix it:")
		GotoShell()
		archivePath = ReadNotEmpty("Created archive file: ")
		if archivePath == "!" {
			fmt.Println("User dropped this record.")
			return
		}
	}
	for !IsAFile(archivePath) {
		fmt.Println("File seems not exists or is a dir, please retry.")
		archivePath = ReadNotEmpty("Created archive file: ")
		if archivePath == "!" {
			fmt.Println("User dropped this record.")
			return
		}
		for !strings.HasSuffix(archivePath, Format) {
			fmt.Println("Format mismatch, might be a mistake, use terminal below to fix it:")
			GotoShell()
			archivePath = ReadNotEmpty("Created archive file: ")
			if archivePath == "!" {
				fmt.Println("User dropped this record.")
				return
			}
		}
	}

	stat, err := os.Stat(archivePath)
	for err != nil {
		fmt.Println("Can not stat archive file, might be a mistake, use terminal below to fix it:")
		GotoShell()
		stat, err = os.Stat(archivePath)
	}
	record.Size = stat.Size()
	fmt.Printf("Archive file %s has %d bytes.\n", archivePath, record.Size)
	fmt.Println("Input SHA-256 of this file or let it empty if you want to calculate it here.")
	var inputedHash string
	for {
		inputedHash = PromptAndReadLine(">>> ")
		inputedHash = strings.ToLower(inputedHash)
		if len(inputedHash) != 64 && len(inputedHash) != 0 {
			fmt.Println("Invalid input, try again.")
			continue
		}
		_, err := hex.DecodeString(inputedHash)
		if err != nil {
			fmt.Println("Invalid input, try again.")
			continue
		}
		break
	}
	if inputedHash == "" {
		fmt.Println("Calculating SHA-256 sum for this file...")
		f, err := os.Open(archivePath)
		for err != nil {
			fmt.Println("Can not open archive file, might be a mistake, use terminal below to fix it:")
			GotoShell()
			f, err = os.Open(archivePath)
		}
		defer func() { _ = f.Close() }()
		hasher := sha256.New()
		_, err = io.Copy(hasher, f)
		for err != nil {
			fmt.Println("Can not hash archive file, might be a mistake, use terminal below to fix it:")
			GotoShell()
			hasher = sha256.New()
			_, err = f.Seek(0, io.SeekStart)
			if err != nil {
				fmt.Println("Record dropped since a critical error when calculating checksum.")
				return
			}
			_, err = io.Copy(hasher, f)
		}
		record.Checksum = fmt.Sprintf("%x", hasher.Sum(nil))
		fmt.Printf("SHA-256 sum of %s is %s.\n", archivePath, record.Checksum)
	} else {
		record.Checksum = inputedHash
	}
	fmt.Println("You can add extra attributes, use empty key to terminate:")
	for {
		k := PromptAndReadLine("Key: ")
		if k == "" {
			break
		}
		v := ReadNotEmpty("Value: ")
		record.ExtraAttributes[k] = v
	}
	fmt.Println("You can add tags, use empty tag to terminate:")
	for {
		tag := PromptAndReadLine(">>> ")
		if tag == "" {
			break
		}
		if slices.Index(record.Tags, tag) != -1 {
			fmt.Println("Already have this tag, ignored.")
			continue
		}
		record.Tags = append(record.Tags, tag)
	}
	fmt.Println("You can add notes, use empty note to terminate:")
	for {
		note := PromptAndReadLine(">>> ")
		if note == "" {
			break
		}
		if slices.Index(record.Notes, note) != -1 {
			fmt.Println("Already have this note, ignored.")
			continue
		}
		record.Notes = append(record.Notes, note)
	}
LoopCapTooSmall:
	for record.Size+PerFileSafetyMarginMiB*1024*1024 >= TapeCapAvailMiB*1024*1024-TapeSafetyMargin {
		fmt.Println("It's not safe to write this archive into current tape.")
		fmt.Println("Input \"T\" to change tape, \"I\" to ignore, \"D\" to drop this record, \"$\" to get shell.")
		switch GetCommand() {
		case "T":
			TapeID = ReadNotEmpty("Input the ID (usually barcode) of target tape (scanner is recommended): ")
			if TapeID == "!" {
				fmt.Println("User dropped this record.")
				return
			}
			TapeCapAvailMiB = -1
			for TapeCapAvailMiB < 0 {
				fmt.Println("Hint: use \"sudo sg_logs -p 0x31 /dev/(n)stX\" to view remaining capacity in MiB.")
				tmp := PromptAndReadLine("Input the remaining capacity of the tape (in MiB): ")
				if tmp == "!" {
					fmt.Println("User dropped this record.")
					return
				}
				tmpNum, err := strconv.Atoi(tmp)
				if err != nil || tmpNum < 0 {
					fmt.Println("Invalid capacity, please try again.")
					continue
				}
				TapeCapAvailMiB = int64(tmpNum)
			}
		case "D", "!":
			fmt.Println("User dropped this record.")
			return
		case "I":
			break LoopCapTooSmall
		default:
			fmt.Println("Unknown choice, try again.")
		}
	}
	fmt.Println("Hint: you can input $ to escape to shell and do some query.")
	fmt.Println("Hint: you can use \"sudo mt-st -f /dev/nstX status\" to see which file you are using now.")
	for {
		tmp := ReadNotEmpty("File index: ")
		if tmp == "!" {
			fmt.Println("User dropped this record.")
			return
		}
		tmpNum, err := strconv.Atoi(tmp)
		if err != nil {
			fmt.Println("Invalid file index, try again.")
			continue
		}
		record.Location.Site = Site
		record.Location.TapeID = TapeID
		record.Location.FileIndex = tmpNum
		break
	}
	fmt.Printf("Now, you can write your archive file (%s) to tape %s, and do something other using terminal below:\n", archivePath, TapeID)
	GotoShell()
	tmp := "NOTEMPTY"
	for tmp != "" {
		tmp = PromptAndReadLine("You can input \"$\" only to reopen shell, or use empty input to escape: ")
		if tmp == "!" {
			fmt.Println("User dropped this record.")
			return
		}
	}
	fmt.Println("Merge or drop this record? M to merge and D to drop.")
LoopMergeOrDrop:
	for {
		tmp = GetCommand()
		switch tmp {
		case "D", "!":
			fmt.Println("User dropped this record.")
			return
		case "M":
			break LoopMergeOrDrop
		default:
			fmt.Println("Unknown choice, try again.")
		}
	}
	TapeCapAvailMiB -= int64(math.Ceil(float64(record.Size)/1024/1024)) + PerFileSafetyMarginMiB
	manifest.Records = append(manifest.Records, record)
}

func RuneWidth(ch rune) int {
	if ch <= 0x7F { // A.K.A. 0~127
		return 1
	} else {
		return 2
	}
}

func StringWidth(s string) int {
	width := 0
	for _, ch := range s {
		width += RuneWidth(ch)
	}
	return width
}

func SPrintTitleln(s string) string {
	res := ""
	if s != "" {
		s = " " + s + " "
	}
	res += fmt.Sprint(strings.Repeat("=", (80-StringWidth(s))/2))
	res += fmt.Sprint(s)
	res += fmt.Sprintln(strings.Repeat("=", 80-StringWidth(res)))
	return res
}

func SPrintCenterln(s string) string {
	if s == "" {
		return "\n"
	}
	res := ""
	res += fmt.Sprint(strings.Repeat(" ", (80-StringWidth(s))/2))
	res += fmt.Sprint(s)
	res += fmt.Sprintln()
	return res
}

func SPrintDoubleColsln(colA, colB string) string {
	res := ""
	res += fmt.Sprint(colA)
	res += fmt.Sprint(strings.Repeat(" ", max(4, 80/2-StringWidth(colA))))
	res += fmt.Sprintln(colB)
	return res
}

func SPrintLongTextln(prefix, text string) string {
	res := ""
	prefixWidth := StringWidth(prefix)
	res += fmt.Sprint(prefix)
	cur := 0
	for _, ch := range text {
		if cur+RuneWidth(ch)+prefixWidth > 80 {
			res += fmt.Sprint("\n" + strings.Repeat(" ", prefixWidth))
			cur = 0
		}
		cur += RuneWidth(ch)
		res += fmt.Sprint(string(ch))
	}
	return res + "\n"
}

func HumanSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.2f KiB", float64(bytes)/1024)
	}
	if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MiB", float64(bytes)/1024/1024)
	}
	if bytes < 1024*1024*1024*1024 {
		return fmt.Sprintf("%.2f GiB", float64(bytes)/1024/1024/1024)
	}
	if bytes < 1024*1024*1024*1024 {
		return fmt.Sprintf("%.2f TiB", float64(bytes)/1024/1024/1024/1024)
	}
	if bytes < 1024*1024*1024*1024*1024 {
		return fmt.Sprintf("%.2f PiB", float64(bytes)/1024/1024/1024/1024/1024)
	}
	if bytes < 1024*1024*1024*1024*1024*1024 {
		return fmt.Sprintf("%.2f EiB", float64(bytes)/1024/1024/1024/1024/1024/1024)
	}
	return fmt.Sprintf("%d B", bytes)
}

func SPrintSha256(s string) string {
	if len(s) != 64 {
		return s
	}
	s = strings.ToUpper(s)
	return fmt.Sprintf("%s %s %s %s", s[0:16], s[16:32], s[32:48], s[48:64])
}

func Export(manifest *Manifest) {
	text := ""
	text += SPrintTitleln("")
	text += SPrintCenterln("Uiharu Tape Archive Helper Catalog")
	text += SPrintTitleln("")
	text += SPrintLongTextln("Manifest Name: ", manifest.ManifestName)
	text += SPrintDoubleColsln("Maintainer: "+manifest.Maintainer, "UTAH Version: "+manifest.UTAHVersion)
	text += SPrintDoubleColsln("Created At: "+manifest.CreatedAt.Format(CatalogTimeFormat), "Last Modified: "+manifest.LastModified.Format(CatalogTimeFormat))
	text += SPrintDoubleColsln("Exported At: "+time.Now().Format(CatalogTimeFormat), "Records Count: "+strconv.Itoa(len(manifest.Records)))
	if manifest.Note != "" {
		text += SPrintLongTextln("Note: ", manifest.Note)
	}
	text += SPrintTitleln("All Archive Names")
	names := make([]string, 0)
	for _, rec := range manifest.Records {
		names = append(names, rec.Name)
	}
	text += SPrintLongTextln("", strings.Join(names, ", "))
	text += SPrintTitleln("Archive Details")
	for cur, rec := range manifest.Records {
		text += SPrintLongTextln("Name: ", rec.Name)
		text += SPrintDoubleColsln("Format: "+rec.Format, "Created At: "+rec.CreatedAt.Format(CatalogTimeFormat))
		text += SPrintDoubleColsln("Archive Size: "+HumanSize(rec.Size), "Stored At: "+rec.Location.Site)
		text += SPrintDoubleColsln("Tape ID: "+rec.Location.TapeID, "File Index: "+strconv.Itoa(rec.Location.FileIndex))
		text += SPrintLongTextln("SHA-256: ", SPrintSha256(rec.Checksum))
		if len(rec.Tags) != 0 {
			text += SPrintLongTextln("Tags: ", strings.Join(rec.Tags, ", "))
		}
		if len(rec.ExtraAttributes) != 0 {
			text += SPrintLongTextln("", "Extra Attributes:")
			i := 0
			for k, v := range rec.ExtraAttributes {
				i++
				text += SPrintLongTextln("["+strconv.Itoa(i)+"] "+k+" = ", v)
			}
		}
		if len(rec.Notes) != 0 {
			text += SPrintLongTextln("", "Notes:")
			for i, note := range rec.Notes {
				text += SPrintLongTextln("["+strconv.Itoa(i+1)+"] ", note)
			}
		}
		if cur+1 < len(manifest.Records) {
			text += SPrintTitleln("")
		}
	}
	text += SPrintTitleln("End Of Uiharu Tape Archive Helper Catalog")

	// Output to file.
	outPath := ReadNotEmpty("Output path (will overwrite if file exists): ")
	f, err := os.Create(outPath)
	if err != nil {
		fmt.Println("Export failed: " + err.Error())
		return
	}
	n, err := f.WriteString(text)
	if err != nil {
		fmt.Println("Export failed: " + err.Error())
		return
	}
	fmt.Printf("Written %d bytes of data.\n", n)
}

func main() {
	fmt.Println("Uiharu Tape Archive Helper [ Version: " + Version + " ]")
	// Pervent the misuse of Ctrl+C.
	signal.Ignore(syscall.SIGTERM, syscall.SIGINT)

	// Check args count.
	if len(os.Args) <= 1 {
		panic("no manifest file specified")
	}

	// Get file path.
	ManifestPath = os.Args[1]

	manifest := Manifest{}

	if !FileOrDirExists(ManifestPath) {
		fmt.Println("Will create a new manifest.")
		manifest.CreatedAt = time.Now()
		manifest.UTAHVersion = Version
		manifest.Records = make([]Record, 0)
		manifest.ManifestName = ReadNotEmpty("Manifest name: ")
		manifest.Maintainer = ReadNotEmpty("Maintainer: ")
		manifest.Note = PromptAndReadLine("Note: ")
		manifest.LastModified = time.Now()
	} else {
		// Read JSON file.
		f, err := os.Open(ManifestPath)
		Check(err)
		defer func() { Check(f.Close()) }()
		data, err := io.ReadAll(f)
		Check(err)
		_ = f.Close()

		// Unmarshal JSON.
		err = json.Unmarshal(data, &manifest)
		Check(err)
	}

	// Set preferences.
	fmt.Println("You need to set preferences before you can start.")
	Preferences()

	// Main loop.
	for {
		fmt.Println("Available operations:")
		fmt.Println("[P]references")
		fmt.Println("[A]dd archive")
		fmt.Println("[E]xport to text")
		fmt.Println("[S]ave")
		fmt.Println("[Q]uit")
		fmt.Println("Hint: Everywhere, input $ only will bring you to shell.")
		fmt.Println("Hint: ONLY in this screen, after you exit shell, if you can't see this, just press Enter ONLY.")
		choice := GetCommand()
		switch choice {
		case "P":
			Preferences()
		case "A":
			Add(&manifest)
		case "E":
			Export(&manifest)
		case "S":
			Save(&manifest)
		case "Q":
			Save(&manifest)
			fmt.Println("Uiharu Tape Archive Helper will exit.")
			os.Exit(0)
		case "":
			// Pass //
		default:
			fmt.Println("Unknown command, please retry.")
		}
	}
}
