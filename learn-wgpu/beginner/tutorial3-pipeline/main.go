package main

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/rajveermalviya/gamen/display"
	"github.com/rajveermalviya/gamen/dpi"
	"github.com/rajveermalviya/go-webgpu/wgpu"
)

//go:embed shader.wgsl
var shaderCode string

type State struct {
	surface   *wgpu.Surface
	swapChain *wgpu.SwapChain
	device    *wgpu.Device
	queue     *wgpu.Queue
	config    *wgpu.SwapChainDescriptor
	size      dpi.PhysicalSize[uint32]

	renderPipeline *wgpu.RenderPipeline
}

func InitState(window display.Window) (s *State, err error) {
	defer func() {
		if err != nil {
			s.Destroy()
			s = nil
		}
	}()
	s = &State{}
	s.size = window.InnerSize()

	instance := wgpu.CreateInstance(nil)

	s.surface = instance.CreateSurface(getSurfaceDescriptor(window))

	adaper, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{
		CompatibleSurface: s.surface,
	})
	if err != nil {
		return s, err
	}
	defer adaper.Drop()

	s.device, err = adaper.RequestDevice(nil)
	if err != nil {
		return s, err
	}
	s.queue = s.device.GetQueue()

	s.config = &wgpu.SwapChainDescriptor{
		Usage:       wgpu.TextureUsage_RenderAttachment,
		Format:      s.surface.GetPreferredFormat(adaper),
		Width:       s.size.Width,
		Height:      s.size.Height,
		PresentMode: wgpu.PresentMode_Fifo,
	}
	s.swapChain, err = s.device.CreateSwapChain(s.surface, s.config)
	if err != nil {
		return s, err
	}

	shader, err := s.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "shader.wgsl",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{
			Code: shaderCode,
		},
	})
	if err != nil {
		return s, err
	}
	defer shader.Drop()

	renderPipelineLayout, err := s.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		Label: "Render Pipeline Layout",
	})
	if err != nil {
		return s, err
	}
	defer renderPipelineLayout.Drop()

	s.renderPipeline, err = s.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "Render Pipeline",
		Layout: renderPipelineLayout,
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: "vs_main",
		},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{{
				Format:    s.config.Format,
				Blend:     &wgpu.BlendState_Replace,
				WriteMask: wgpu.ColorWriteMask_All,
			}},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopology_TriangleList,
			FrontFace: wgpu.FrontFace_CCW,
			CullMode:  wgpu.CullMode_Back,
		},
		Multisample: wgpu.MultisampleState{
			Count:                  1,
			Mask:                   0xFFFFFFFF,
			AlphaToCoverageEnabled: false,
		},
	})
	if err != nil {
		return s, err
	}

	return s, nil
}

func (s *State) Resize(newSize dpi.PhysicalSize[uint32]) {
	if newSize.Width > 0 && newSize.Height > 0 {
		s.size = newSize
		s.config.Width = newSize.Width
		s.config.Height = newSize.Height

		if s.swapChain != nil {
			s.swapChain.Drop()
		}
		var err error
		s.swapChain, err = s.device.CreateSwapChain(s.surface, s.config)
		if err != nil {
			panic(err)
		}
	}
}

func (s *State) Render() error {
	view, err := s.swapChain.GetCurrentTextureView()
	if err != nil {
		return err
	}
	defer view.Drop()

	encoder, err := s.device.CreateCommandEncoder(nil)
	if err != nil {
		return err
	}

	renderPass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:   view,
			LoadOp: wgpu.LoadOp_Clear,
			ClearValue: wgpu.Color{
				R: 0.1,
				G: 0.2,
				B: 0.3,
				A: 1.0,
			},
			StoreOp: wgpu.StoreOp_Store,
		}},
	})
	renderPass.SetPipeline(s.renderPipeline)
	renderPass.Draw(3, 1, 0, 0)
	renderPass.End()

	s.queue.Submit(encoder.Finish(nil))
	s.swapChain.Present()

	return nil
}

func (s *State) Destroy() {
	if s.renderPipeline != nil {
		s.renderPipeline.Drop()
		s.renderPipeline = nil
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
}

func main() {
	d, err := display.NewDisplay()
	if err != nil {
		panic(err)
	}
	defer d.Destroy()

	w, err := display.NewWindow(d)
	if err != nil {
		panic(err)
	}
	defer w.Destroy()

	s, err := InitState(w)
	if err != nil {
		panic(err)
	}
	defer s.Destroy()

	w.SetResizedCallback(func(physicalWidth, physicalHeight uint32, scaleFactor float64) {
		s.Resize(dpi.PhysicalSize[uint32]{
			Width:  physicalWidth,
			Height: physicalHeight,
		})
	})

	w.SetCloseRequestedCallback(func() {
		d.Destroy()
	})

	for {
		if !d.Poll() {
			break
		}

		err := s.Render()
		if err != nil {
			errstr := err.Error()
			fmt.Println(errstr)

			switch {
			case strings.Contains(errstr, "Lost"):
				s.Resize(s.size)
			case strings.Contains(errstr, "Outdated"):
				s.Resize(s.size)
			case strings.Contains(errstr, "Timeout"):
			default:
				panic(err)
			}
		}
	}
}
