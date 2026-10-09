package main

import (
	_ "github.com/suxinwl/GoSuxin/internal/addons/ebook"
	"github.com/suxinwl/GoSuxin/internal/pluginworker"
)

func main() { pluginworker.Run("ebook", nil) }
