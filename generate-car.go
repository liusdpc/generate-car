package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path"
	"strconv"
	"time"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/tech-greedy/go-generate-car/util"
	"github.com/urfave/cli/v2"
)

type CommpResult struct {
	commp     string
	pieceSize uint64
}

type Result struct {
	Ipld      *util.FsNode
	// FileSize  uint64
	DataCid   string
	PieceCid  string
	PieceSize uint64
	CidMap    map[string]util.CidMapValue
}

type Input []util.Finfo

type CarHeader struct {
	Roots   []cid.Cid
	Version uint64
}

func init() {
	cbor.RegisterCborType(CarHeader{})
}

const BufSize = (4 << 20) / 128 * 127

func main() {
	ctx := context.TODO()
	app := &cli.App{
		Name:  "generate-car",
		Usage: "generate car archive from list of files and compute commp in the mean time",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "input",
				Aliases: []string{"i"},
				Usage:   "File to read list of files, or '-' if from stdin",
				Value:   "-",
			},
			&cli.Uint64Flag{
				Name:    "quantity",
				Aliases: []string{"q"},
				Usage:   "Quantiry of car files",
				Value:   3,
			},
			&cli.Uint64Flag{
				Name:    "file-size",
				Usage:   "Target car file size, default to 32GiB size sector",
				Value:   19327352832,
				// Value: 17584454,
			},
			&cli.Uint64Flag{
				Name:    "piece-size",
				Aliases: []string{"s"},
				Usage:   "Target piece size, default to minimum possible value",
				Value:   34359738368,
			},
			&cli.StringFlag{
				Name:    "out-file",
				Usage:   "Output file as .csv format to save the car file",
				Value:   "source.csv",
			},
			&cli.StringFlag{
				Name:    "out-dir",
				Aliases: []string{"o"},
				Usage:   "Output directory to save the car file",
				Value:   ".",
			},
			&cli.StringFlag{
				Name:    "tmp-dir",
				Aliases: []string{"t"},
				Usage:   "Optionally copy the files to a temporary (and much faster) directory",
				Value:   "",
			},
			&cli.StringFlag{
				Name:     "parent",
				Aliases:  []string{"p"},
				Usage:    "Parent path of the dataset",
				Required: true,
			},
		},
		Action: func(c *cli.Context) error {
			inputFile := c.String("input")
			fileSizeInput := c.Uint64("file-size")
			pieceSizeInput := c.Uint64("piece-size")
			quantity := c.Uint64("quantity")
			outFile := c.String("out-file")
			outDir := c.String("out-dir")
			parent := c.String("parent")
			tmpDir := c.String("tmp-dir")
			var inputBytes []byte
			if inputFile == "-" {
				reader := bufio.NewReader(os.Stdin)
				buf := new(bytes.Buffer)
				_, err := buf.ReadFrom(reader)
				if err != nil {
					return err
				}
				inputBytes = buf.Bytes()
			} else {
				bytes, err := os.ReadFile(inputFile)
				if err != nil {
					return err
				}
				inputBytes = bytes
			}

			var inputFiles Input
			err := json.Unmarshal(inputBytes, &inputFiles)
			if err != nil {
				return err
			}

			csvF, err := os.Create(outDir + "/" + outFile)
			defer csvF.Close()
			if err != nil {
				return err
			}

			for i := 0; i < int(quantity); i++ {
				start := time.Now()
				var selectedFiles []util.Finfo
				totalSize := 0
				rand.Seed(time.Now().Unix())
				for totalSize < int(fileSizeInput) {
					choicedFile := inputFiles[rand.Intn(len(inputFiles))]
					totalSize += int(choicedFile.Size)
					fileInfo := choicedFile
					selectedFiles = append(selectedFiles, fileInfo)
				}
	
				fmt.Printf("Will generate file with %d bytes\n", totalSize)
	
				outFilename := uuid.New().String() + ".car"
				outPath := path.Join(outDir, outFilename)
				carF, err := os.Create(outPath)
				if err != nil {
					return err
				}
				cp := new(commp.Calc)
				writer := bufio.NewWriterSize(io.MultiWriter(carF, cp), BufSize)
				_, cid, _, err := util.GenerateCar(ctx, selectedFiles, parent, tmpDir, writer)
				if err != nil {
					return err
				}
				err = writer.Flush()
				if err != nil {
					return err
				}
				err = carF.Close()
				if err != nil {
					return err
				}
				rawCommP, pieceSize, err := cp.Digest()
				if err != nil {
					return err
				}
				if pieceSizeInput > 0 {
					rawCommP, err = commp.PadCommP(
						rawCommP,
						pieceSize,
						pieceSizeInput,
					)
					if err != nil {
						return err
					}
					pieceSize = pieceSizeInput
				}
				commCid, err := commcid.DataCommitmentV1ToCID(rawCommP)
				if err != nil {
					return err
				}

				generatedFile := outPath
				// err = os.Rename(outPath, generatedFile)
				// if err != nil {
				// 	return err
				// }
				elapsed := time.Since(start)
				fmt.Printf("Generated %d car %s took %s \n", i, generatedFile, elapsed)

				// get car file size
				carFi, err := os.Stat(generatedFile)
				if err != nil {
					return err
				}

				outItem := []string{
					commCid.String(),
					strconv.Itoa(int(carFi.Size())),
					strconv.Itoa(int(pieceSize)),
					cid,
				}

				csvWtiter := csv.NewWriter(csvF)
				csvWtiter.Write(outItem)
				csvWtiter.Flush()

				fmt.Printf("Push %s to out file \n", commCid.String())
			}
			return nil
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
