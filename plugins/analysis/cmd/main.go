package main

import (
	_ "github.com/suxinwl/GoSuxin/internal/addons/analysis"
	"github.com/suxinwl/GoSuxin/internal/pluginworker"
)

func main() { pluginworker.Run("analysis", nil) }
