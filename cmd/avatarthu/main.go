package main

import (
	"github.com/Sskift/AvatarTHU/internal/app"
	"os"
)

func main() { os.Exit(app.Main(os.Args[1:])) }
