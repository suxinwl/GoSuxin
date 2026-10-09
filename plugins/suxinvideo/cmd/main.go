package main

import (
	"github.com/suxinwl/GoSuxin/internal/controller/suxinvideo"
	"github.com/suxinwl/GoSuxin/internal/pluginworker"
)

func main() { pluginworker.Run("suxinvideo", suxinvideo.R.BindController) }
