package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Planxnx/eth-wallet-gen/pkg/wallets"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/Planxnx/eth-wallet-gen/pkg/progressbar"
)

var (
	wg     sync.WaitGroup
	result strings.Builder
)

//Wallet ethereum wallet data
type Wallet struct {
	Address    string
	PrivateKey string
	Mnemonic   string
	Bits       int
	HDPath     string
	CreatedAt  time.Time
	gorm.Model
}

func generateNewWallet(bits int) *wallets.Wallet {
	wallet, err := wallets.NewWallet(bits)
	if err != nil {
		panic(err)
	}
	return wallet
}

func init() {
	if _, err := os.Stat("db"); os.IsNotExist(err) {
		if err := os.Mkdir("db", 0750); err != nil {
			panic(err)
		}
	}
}

func main() {

	interrupt := make(chan os.Signal, 1)

	signal.Notify(
		interrupt,
		syscall.SIGHUP,  // kill -SIGHUP XXXX
		syscall.SIGINT,  // kill -SIGINT XXXX or Ctrl+c
		syscall.SIGQUIT, // kill -SIGQUIT XXXX
		syscall.SIGTERM, // kill -SIGTERM XXXX
	)

	fmt.Println("===============ETH Wallet Generator===============")
	fmt.Println(" ")

	number := flag.Int("n", 10, "set number of wallets to generate (set number to -1 for Infinite loop ∞)")
	dbPath := flag.String("db", "", "set sqlite output name eg. wallets.db (db file will create in /db)")
	concurrency := flag.Int("c", 1, "set concurrency value")
	bits := flag.Int("bit", 256, "set number of entropy bits [128, 256]")
	strict := flag.Bool("strict", false, "strict contains mode (required contains to use)")
	contain := flag.String("contains", "", "used to check if the given letters are present in the given string")
	prefix := flag.String("prefix", "", "used to check if the given letters are present in the prefix string")
	suffix := flag.String("suffix", "", "used to check if the given letters are present in the suffix string")
	regEx := flag.String("regex", "", "used to check if the given letters are present in the regex format")
	isDryrun := flag.Bool("dryrun", false, "generate wallet without a result (used for benchmark speed)")
	isCompatible := flag.Bool("compatible", false, "logging compatible mode (turn this on to fix logging glitch)")
	flag.Parse()

	r, err := regexp.Compile(*regEx)
	if err != nil {
		panic(err)
	}
	contains := strings.Split(*contain, ",")
	validateAddress := func(address string) bool {
		isValid := true
		if len(contains) > 0 {
			cb := func(contain string) bool {
				return strings.Contains(address, contain)
			}
			if *strict {
				if !have(contains, cb) {
					isValid = false
				}
			} else {
				if !some(contains, cb) {
					isValid = false
				}
			}
		}

		if *prefix != "" {
			if !strings.HasPrefix(address, *prefix) {
				isValid = false
			}
		}

		if *suffix != "" {
			if !strings.HasSuffix(address, *suffix) {
				isValid = false
			}
		}

		if *regEx != "" && !r.MatchString(address) {
			isValid = false
		}

		return isValid
	}
	if *number < 0 {
		*number = -1
	}

	now := time.Now()
	resolvedCount := 0

	defer func() {
		fmt.Printf("\nResolved Speed: %.2f w/s\n", float64(resolvedCount)/time.Since(now).Seconds())
		fmt.Printf("Total Duration: %v\n", time.Since(now))
		fmt.Printf("Total Wallet Resolved: %d w\n", resolvedCount)

		fmt.Printf("\nCopyright (C) 2023 Planxnx <planxthanee@gmail.com>\n")
	}()

	var bar *progressbar.ProgressBar
	if *isCompatible {
		bar = progressbar.NewCompatibleProgressBar(*number)
	} else {
		bar = progressbar.NewStandardProgressBar(*number)
	}

	defer func() {
		_ = bar.Finish()
		if *isDryrun {
			return
		}
		if result.Len() > 0 {
			fmt.Printf("\n%-42s %s\n", "Address", "Seed")
			fmt.Printf("%-42s %s\n", strings.Repeat("-", 42), strings.Repeat("-", 160))
			fmt.Println(result.String())
		}
	}()

	go func() {
		defer func() {
			interrupt <- syscall.SIGQUIT
		}()

		// generate wallets with db
		if *dbPath != "" {
			db, err := gorm.Open(sqlite.Open("./db/"+*dbPath), &gorm.Config{
				Logger:                 logger.Default.LogMode(logger.Silent),
				DryRun:                 *isDryrun,
				SkipDefaultTransaction: true,
			})
			if err != nil {
				panic(err)
			}

			if !*isDryrun {
				if err := db.AutoMigrate(&Wallet{}); err != nil {
					panic(err)
				}
			}

			for i := 0; i < *number || *number < 0; i += *concurrency {
				tx := db.Begin() //Optimized Performance
				txResolved := 0
				for j := 0; j < *concurrency && (i+j < *number || *number < 0); j++ {
					wg.Add(1)

					go func(j int) {
						defer wg.Done()

						wallet := generateNewWallet(*bits)
						_ = bar.Increment()

						if !validateAddress(wallet.Address) {
							return
						}

						txResolved++
						tx.Create(wallet)
					}(j)
				}
				wg.Wait()
				tx.Commit()
				resolvedCount += txResolved
				_ = bar.SetResolved(resolvedCount)
			}
			return
		}

		// generate wallets without db
		semph := make(chan int, *concurrency)
		for i := 0; i < *number || *number < 0; i++ {
			semph <- 1
			wg.Add(1)

			go func(i int) {
				defer func() {
					<-semph
					wg.Done()
				}()

				wallet := generateNewWallet(*bits)
				_ = bar.Increment()

				// if *contain != "" && !strings.Contains(wallet.Address, *contain) {
				// 	return
				// }

				if !validateAddress(wallet.Address) {
					return
				}

				fmt.Fprintf(&result, "%-18s %s\n", wallet.Address, wallet.Mnemonic)
				resolvedCount++
				_ = bar.SetResolved(resolvedCount)
			}(i)
		}
		wg.Wait()
		_ = bar.Finish()
	}()
	<-interrupt
}

// forked this methods from core-js
func some(arr []string, fn func(string) bool) bool {
	for _, v := range arr {
		if fn(v) {
			return true
		}
	}
	return false
}

func have(arr []string, fn func(string) bool) bool {
	for _, v := range arr {
		if !fn(v) {
			return false
		}
	}
	return true
}
