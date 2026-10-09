package main

import (
	_ "github.com/suxinwl/GoSuxin/internal/addons/privatecode"
	"github.com/suxinwl/GoSuxin/internal/pluginworker"
)

func main() { pluginworker.Run("privatecode", nil) }
