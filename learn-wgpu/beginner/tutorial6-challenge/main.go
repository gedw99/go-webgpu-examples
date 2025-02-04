package main

import (
	_ "embed"
	"fmt"
	"strings"
	"unsafe"

	"github.com/rajveermalviya/gamen/display"
	"github.com/rajveermalviya/gamen/dpi"
	"github.com/rajveermalviya/gamen/events"
	"github.com/rajveermalviya/go-webgpu-examples/internal/glm"
	"github.com/rajveermalviya/go-webgpu/wgpu"
)

//go:embed shader.wgsl
var shaderCode string

//go:embed happy-tree.png
var happyTreePng []byte

type Vertex struct {
	position  [3]float32
	texCoords [2]float32
}

var VertexBufferLayout = wgpu.VertexBufferLayout{
	ArrayStride: uint64(unsafe.Sizeof(Vertex{})),
	StepMode:    wgpu.VertexStepMode_Vertex,
	Attributes: []wgpu.VertexAttribute{
		{
			Offset:         0,
			ShaderLocation: 0,
			Format:         wgpu.VertexFormat_Float32x3,
		},
		{
			Offset:         uint64(unsafe.Sizeof([3]float32{})),
			ShaderLocation: 1,
			Format:         wgpu.VertexFormat_Float32x2,
		},
	},
}

var VERTICES = [...]Vertex{
	{
		position:  [3]float32{-0.0868241, 0.49240386, 0.0},
		texCoords: [2]float32{0.4131759, 0.00759614},
	}, // A
	{
		position:  [3]float32{-0.49513406, 0.06958647, 0.0},
		texCoords: [2]float32{0.0048659444, 0.43041354},
	}, // B
	{
		position:  [3]float32{-0.21918549, -0.44939706, 0.0},
		texCoords: [2]float32{0.28081453, 0.949397},
	}, // C
	{
		position:  [3]float32{0.35966998, -0.3473291, 0.0},
		texCoords: [2]float32{0.85967, 0.84732914},
	}, // D
	{
		position:  [3]float32{0.44147372, 0.2347359, 0.0},
		texCoords: [2]float32{0.9414737, 0.2652641},
	}, // E
}

var INDICES = [...]uint16{0, 1, 4, 1, 2, 4, 2, 3, 4}

var OpenGlToWgpuMatrix = glm.Mat4[float32]{
	1.0, 0.0, 0.0, 0.0,
	0.0, 1.0, 0.0, 0.0,
	0.0, 0.0, 0.5, 0.0,
	0.0, 0.0, 0.5, 1.0,
}

type Camera struct {
	eye     glm.Vec3[float32]
	target  glm.Vec3[float32]
	up      glm.Vec3[float32]
	aspect  float32
	fovYRad float32
	znear   float32
	zfar    float32
}

func (c *Camera) buildViewProjectionMatrix() glm.Mat4[float32] {
	view := glm.LookAtRH(c.eye, c.target, c.up)
	proj := glm.Perspective(c.fovYRad, c.aspect, c.znear, c.zfar)
	return proj.Mul4(view)
}

type CameraStaging struct {
	camera           *Camera
	modelRotationDeg float32
}

func NewCameraStaging(camera *Camera) *CameraStaging {
	return &CameraStaging{
		camera:           camera,
		modelRotationDeg: 0,
	}
}

func (c *CameraStaging) UpdateCamera(cameraUniform *CameraUniform) {
	cameraUniform.modelViewProj = OpenGlToWgpuMatrix.
		Mul4(c.camera.buildViewProjectionMatrix()).
		Mul4(glm.Mat4FromAngleZ(glm.DegToRad(c.modelRotationDeg)))
}

type CameraUniform struct {
	modelViewProj glm.Mat4[float32]
}

func NewCameraUnifrom() *CameraUniform {
	return &CameraUniform{
		modelViewProj: glm.Mat4[float32]{
			1, 0, 0, 0,
			0, 1, 0, 0,
			0, 0, 1, 0,
			0, 0, 0, 1,
		},
	}
}

type CameraController struct {
	speed             float32
	isForwardPressed  bool
	isBackwardPressed bool
	isLeftPressed     bool
	isRightPressed    bool
}

func NewCameraController(speed float32) *CameraController {
	return &CameraController{speed: speed}
}

func (c *CameraController) UpdateCamera(camera *Camera) {
	forward := camera.target.Sub(camera.eye)
	forwardNorm := forward.Normalize()
	forwardMag := forward.Magnitude()

	if c.isForwardPressed && forwardMag > c.speed {
		camera.eye = camera.eye.Add(forwardNorm.MulScalar(c.speed))
	}
	if c.isBackwardPressed {
		camera.eye = camera.eye.Sub(forwardNorm.MulScalar(c.speed))
	}

	right := forwardNorm.Cross(camera.up)

	forward = camera.target.Sub(camera.eye)
	forwardMag = forward.Magnitude()

	if c.isRightPressed {
		camera.eye = camera.target.Sub(forward.Add(right.MulScalar(c.speed)).Normalize().MulScalar(forwardMag))
	}
	if c.isLeftPressed {
		camera.eye = camera.target.Sub(forward.Sub(right.MulScalar(c.speed)).Normalize().MulScalar(forwardMag))
	}
}

type State struct {
	surface          *wgpu.Surface
	swapChain        *wgpu.SwapChain
	device           *wgpu.Device
	queue            *wgpu.Queue
	config           *wgpu.SwapChainDescriptor
	size             dpi.PhysicalSize[uint32]
	renderPipeline   *wgpu.RenderPipeline
	vertexBuffer     *wgpu.Buffer
	indexBuffer      *wgpu.Buffer
	numIndices       uint32
	diffuseTexture   *Texture
	diffuseBindGroup *wgpu.BindGroup
	cameraController *CameraController
	cameraUniform    *CameraUniform
	cameraBuffer     *wgpu.Buffer
	cameraBindGroup  *wgpu.BindGroup

	cameraStaging *CameraStaging
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
	defer instance.Drop()

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

	s.diffuseTexture, err = TextureFromPNGBytes(s.device, s.queue, happyTreePng, "happy-tree.png")
	if err != nil {
		return s, err
	}

	textureBindGroupLayout, err := s.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStage_Fragment,
				Texture: wgpu.TextureBindingLayout{
					Multisampled:  false,
					ViewDimension: wgpu.TextureViewDimension_2D,
					SampleType:    wgpu.TextureSampleType_Float,
				},
			},
			{
				Binding:    1,
				Visibility: wgpu.ShaderStage_Fragment,
				Sampler: wgpu.SamplerBindingLayout{
					Type: wgpu.SamplerBindingType_Filtering,
				},
			},
		},
		Label: "TextureBindGroupLayout",
	})
	if err != nil {
		return s, err
	}
	defer textureBindGroupLayout.Drop()

	s.diffuseBindGroup, err = s.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: textureBindGroupLayout,
		Entries: []wgpu.BindGroupEntry{
			{
				Binding:     0,
				TextureView: s.diffuseTexture.view,
			},
			{
				Binding: 1,
				Sampler: s.diffuseTexture.sampler,
			},
		},
		Label: "DiffuseBindGroup",
	})
	if err != nil {
		return s, err
	}

	camera := &Camera{
		eye:     glm.Vec3[float32]{0, 1, 2},
		target:  glm.Vec3[float32]{0, 0, 0},
		up:      glm.Vec3[float32]{0, 1, 0},
		aspect:  float32(s.size.Width) / float32(s.size.Height),
		fovYRad: glm.DegToRad[float32](45),
		znear:   0.1,
		zfar:    100.0,
	}
	s.cameraController = NewCameraController(0.2)
	s.cameraUniform = NewCameraUnifrom()
	s.cameraStaging = NewCameraStaging(camera)
	s.cameraStaging.UpdateCamera(s.cameraUniform)

	s.cameraBuffer, err = s.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Camera Buffer",
		Contents: wgpu.ToBytes(s.cameraUniform.modelViewProj[:]),
		Usage:    wgpu.BufferUsage_Uniform | wgpu.BufferUsage_CopyDst,
	})
	if err != nil {
		return s, err
	}

	cameraBindGroupLayout, err := s.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "CameraBindGroupLayout",
		Entries: []wgpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: wgpu.ShaderStage_Vertex,
			Buffer: wgpu.BufferBindingLayout{
				Type:             wgpu.BufferBindingType_Uniform,
				HasDynamicOffset: false,
				MinBindingSize:   wgpu.WholeSize,
			},
		}},
	})
	if err != nil {
		return s, err
	}
	defer cameraBindGroupLayout.Drop()

	s.cameraBindGroup, err = s.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "CameraBindGroup",
		Layout: cameraBindGroupLayout,
		Entries: []wgpu.BindGroupEntry{{
			Binding: 0,
			Buffer:  s.cameraBuffer,
			Size:    wgpu.WholeSize,
		}},
	})
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
		BindGroupLayouts: []*wgpu.BindGroupLayout{
			textureBindGroupLayout, cameraBindGroupLayout,
		},
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
			Buffers:    []wgpu.VertexBufferLayout{VertexBufferLayout},
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

	s.vertexBuffer, err = s.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Vertex Buffer",
		Contents: wgpu.ToBytes(VERTICES[:]),
		Usage:    wgpu.BufferUsage_Vertex,
	})
	if err != nil {
		return s, err
	}

	s.indexBuffer, err = s.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Index Buffer",
		Contents: wgpu.ToBytes(INDICES[:]),
		Usage:    wgpu.BufferUsage_Index,
	})
	if err != nil {
		return s, err
	}
	s.numIndices = uint32(len(INDICES))

	return s, nil
}

func (s *State) Update() {
	s.cameraController.UpdateCamera(s.cameraStaging.camera)
	s.cameraStaging.modelRotationDeg += 2
	s.cameraStaging.UpdateCamera(s.cameraUniform)
	s.queue.WriteBuffer(s.cameraBuffer, 0, wgpu.ToBytes(s.cameraUniform.modelViewProj[:]))
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

		s.cameraStaging.camera.aspect = float32(newSize.Width) / float32(newSize.Height)
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
	renderPass.SetBindGroup(0, s.diffuseBindGroup, nil)
	renderPass.SetBindGroup(1, s.cameraBindGroup, nil)
	renderPass.SetVertexBuffer(0, s.vertexBuffer, 0, wgpu.WholeSize)
	renderPass.SetIndexBuffer(s.indexBuffer, wgpu.IndexFormat_Uint16, 0, wgpu.WholeSize)
	renderPass.DrawIndexed(s.numIndices, 1, 0, 0, 0)
	renderPass.End()

	s.queue.Submit(encoder.Finish(nil))
	s.swapChain.Present()

	return nil
}

func (s *State) Destroy() {
	if s.indexBuffer != nil {
		s.indexBuffer.Drop()
		s.indexBuffer = nil
	}
	if s.vertexBuffer != nil {
		s.vertexBuffer.Drop()
		s.vertexBuffer = nil
	}
	if s.renderPipeline != nil {
		s.renderPipeline.Drop()
		s.renderPipeline = nil
	}
	if s.cameraBindGroup != nil {
		s.cameraBindGroup.Drop()
		s.cameraBindGroup = nil
	}
	if s.cameraBuffer != nil {
		s.cameraBuffer.Drop()
		s.cameraBuffer = nil
	}
	if s.cameraStaging != nil {
		s.cameraStaging = nil
	}
	if s.cameraUniform != nil {
		s.cameraUniform = nil
	}
	if s.cameraController != nil {
		s.cameraController = nil
	}
	if s.diffuseBindGroup != nil {
		s.diffuseBindGroup.Drop()
		s.diffuseBindGroup = nil
	}
	if s.diffuseTexture != nil {
		s.diffuseTexture.Destroy()
		s.diffuseTexture = nil
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

	w.SetKeyboardInputCallback(func(state events.ButtonState, scanCode events.ScanCode, virtualKeyCode events.VirtualKey) {
		isPressed := state == events.ButtonStatePressed

		switch virtualKeyCode {
		case events.VirtualKeyW, events.VirtualKeyUp:
			s.cameraController.isForwardPressed = isPressed
		case events.VirtualKeyA, events.VirtualKeyLeft:
			s.cameraController.isLeftPressed = isPressed
		case events.VirtualKeyS, events.VirtualKeyDown:
			s.cameraController.isBackwardPressed = isPressed
		case events.VirtualKeyD, events.VirtualKeyRight:
			s.cameraController.isRightPressed = isPressed
		}
	})

	w.SetCloseRequestedCallback(func() {
		d.Destroy()
	})

	for {
		if !d.Poll() {
			break
		}

		s.Update()
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
