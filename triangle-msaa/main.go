package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/rajveermalviya/go-webgpu/wgpu"

	_ "embed"
)

var forceFallbackAdapter = os.Getenv("WGPU_FORCE_FALLBACK_ADAPTER") == "1"

func init() {
	runtime.LockOSThread()

	switch os.Getenv("WGPU_LOG_LEVEL") {
	case "OFF":
		wgpu.SetLogLevel(wgpu.LogLevel_Off)
	case "ERROR":
		wgpu.SetLogLevel(wgpu.LogLevel_Error)
	case "WARN":
		wgpu.SetLogLevel(wgpu.LogLevel_Warn)
	case "INFO":
		wgpu.SetLogLevel(wgpu.LogLevel_Info)
	case "DEBUG":
		wgpu.SetLogLevel(wgpu.LogLevel_Debug)
	case "TRACE":
		wgpu.SetLogLevel(wgpu.LogLevel_Trace)
	}
}

//go:embed shader.wgsl
var shader string

type State struct {
	instance                *wgpu.Instance
	surface                 *wgpu.Surface
	swapChain               *wgpu.SwapChain
	device                  *wgpu.Device
	queue                   *wgpu.Queue
	config                  *wgpu.SwapChainDescriptor
	pipeline                *wgpu.RenderPipeline
	multisampledFramebuffer *wgpu.TextureView
}

func InitState(window *glfw.Window) (s *State, err error) {
	defer func() {
		if err != nil {
			s.Destroy()
			s = nil
		}
	}()
	s = &State{}

	s.instance = wgpu.CreateInstance(nil)

	s.surface = s.instance.CreateSurface(getSurfaceDescriptor(window))

	adapter, err := s.instance.RequestAdapter(&wgpu.RequestAdapterOptions{
		ForceFallbackAdapter: forceFallbackAdapter,
		CompatibleSurface:    s.surface,
	})
	if err != nil {
		return s, err
	}
	defer adapter.Drop()

	s.device, err = adapter.RequestDevice(nil)
	if err != nil {
		return s, err
	}
	s.queue = s.device.GetQueue()

	shader, err := s.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "shader.wgsl",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shader},
	})
	if err != nil {
		return s, err
	}
	defer shader.Drop()

	width, height := window.GetSize()
	s.config = &wgpu.SwapChainDescriptor{
		Usage:       wgpu.TextureUsage_RenderAttachment,
		Format:      s.surface.GetPreferredFormat(adapter),
		Width:       uint32(width),
		Height:      uint32(height),
		PresentMode: wgpu.PresentMode_Fifo,
	}
	s.swapChain, err = s.device.CreateSwapChain(s.surface, s.config)
	if err != nil {
		return s, err
	}

	s.pipeline, err = s.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label: "Render Pipeline",
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: "vs_main",
		},
		Primitive: wgpu.PrimitiveState{
			Topology:         wgpu.PrimitiveTopology_TriangleList,
			StripIndexFormat: wgpu.IndexFormat_Undefined,
			FrontFace:        wgpu.FrontFace_CCW,
			CullMode:         wgpu.CullMode_None,
		},
		Multisample: wgpu.MultisampleState{
			Count:                  4,
			Mask:                   0xFFFFFFFF,
			AlphaToCoverageEnabled: false,
		},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{
				{
					Format:    s.config.Format,
					Blend:     &wgpu.BlendState_Replace,
					WriteMask: wgpu.ColorWriteMask_All,
				},
			},
		},
	})
	if err != nil {
		return s, err
	}

	s.multisampledFramebuffer, err = getMultisampledFramebuffer(
		s.device,
		s.config.Width,
		s.config.Height,
		s.config.Format,
	)
	if err != nil {
		return s, err
	}

	return s, nil
}

func (s *State) Resize(width, height int) {
	if width > 0 && height > 0 {
		s.config.Width = uint32(width)
		s.config.Height = uint32(height)

		if s.swapChain != nil {
			s.swapChain.Drop()
		}
		var err error
		s.swapChain, err = s.device.CreateSwapChain(s.surface, s.config)
		if err != nil {
			panic(err)
		}

		s.multisampledFramebuffer.Drop()
		s.multisampledFramebuffer, err = getMultisampledFramebuffer(
			s.device,
			s.config.Width,
			s.config.Height,
			s.config.Format,
		)
		if err != nil {
			panic(err)
		}
	}
}

func (s *State) Render() error {
	nextTexture, err := s.swapChain.GetCurrentTextureView()
	if err != nil {
		return err
	}
	defer nextTexture.Drop()

	encoder, err := s.device.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{
		Label: "Command Encoder",
	})
	if err != nil {
		return err
	}

	renderPass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:          s.multisampledFramebuffer,
				ResolveTarget: nextTexture,
				LoadOp:        wgpu.LoadOp_Clear,
				StoreOp:       wgpu.StoreOp_Discard,
				ClearValue:    wgpu.Color_Green,
			},
		},
	})

	renderPass.SetPipeline(s.pipeline)
	renderPass.Draw(3, 1, 0, 0)
	renderPass.End()

	s.queue.Submit(encoder.Finish(nil))
	s.swapChain.Present()

	return nil
}

func (s *State) Destroy() {
	if s.multisampledFramebuffer != nil {
		s.multisampledFramebuffer.Drop()
		s.multisampledFramebuffer = nil
	}
	if s.pipeline != nil {
		s.pipeline.Drop()
		s.pipeline = nil
	}
	if s.swapChain != nil {
		s.swapChain.Drop()
		s.swapChain = nil
	}
	if s.config != nil {
		s.config = nil
	}
	if s.queue != nil {
		s.queue = nil
	}
	if s.device != nil {
		s.device.Drop()
		s.device = nil
	}
	if s.surface != nil {
		s.surface.Drop()
		s.surface = nil
	}
	if s.instance != nil {
		s.instance.Drop()
		s.instance = nil
	}
}

func main() {
	if err := glfw.Init(); err != nil {
		panic(err)
	}
	defer glfw.Terminate()

	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	window, err := glfw.CreateWindow(640, 480, "go-webgpu with glfw", nil, nil)
	if err != nil {
		panic(err)
	}
	defer window.Destroy()

	s, err := InitState(window)
	if err != nil {
		panic(err)
	}
	defer s.Destroy()

	window.SetKeyCallback(func(w *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
		// Print resource usage on pressing 'R'
		if key == glfw.KeyR && (action == glfw.Press || action == glfw.Repeat) {
			report := s.instance.GenerateReport()
			buf, _ := json.MarshalIndent(report, "", "  ")
			fmt.Print(string(buf))
		}
	})

	window.SetSizeCallback(func(_ *glfw.Window, width, height int) {
		s.Resize(width, height)
	})

	for !window.ShouldClose() {
		glfw.PollEvents()

		err := s.Render()
		if err != nil {
			fmt.Println(err)

			errstr := err.Error()
			switch {
			case strings.Contains(errstr, "Lost"):
				s.Resize(window.GetSize())
			case strings.Contains(errstr, "Outdated"):
				s.Resize(window.GetSize())
			case strings.Contains(errstr, "Timeout"):
			default:
				panic(err)
			}
		}
	}
}

func getMultisampledFramebuffer(device *wgpu.Device, width, height uint32, format wgpu.TextureFormat) (*wgpu.TextureView, error) {
	texture, err := device.CreateTexture(&wgpu.TextureDescriptor{
		Usage:     wgpu.TextureUsage_RenderAttachment,
		Dimension: wgpu.TextureDimension_2D,
		Size: wgpu.Extent3D{
			Width:              width,
			Height:             height,
			DepthOrArrayLayers: 1,
		},
		Format:        format,
		MipLevelCount: 1,
		SampleCount:   4,
	})
	if err != nil {
		return nil, err
	}
	defer texture.Drop()

	return texture.CreateView(nil), nil
}
