package upscale_test

import (
	"bytes"
	json "encoding/json/v2"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/avienor/bediz/internal/graphops"
	"github.com/avienor/bediz/internal/httpclient"
	"github.com/avienor/bediz/internal/images"
	"github.com/avienor/bediz/internal/operation"
	"github.com/avienor/bediz/internal/upscale"
)

func TestValidateRequestRejectsBoundsBeforeNetwork(t *testing.T) {
	base := upscale.Request{SchemaVersion: 1, Source: upscale.Source{Type: "image", Reference: "source.png"}, Model: "main"}
	tests := []struct {
		name   string
		change func(*upscale.Request)
	}{
		{"scale", func(r *upscale.Request) { r.Scale = new(3) }},
		{"creativity", func(r *upscale.Request) { r.Creativity = new(11) }},
		{"structure", func(r *upscale.Request) { r.Structure = new(-11) }},
		{"steps", func(r *upscale.Request) { r.Steps = new(0) }},
		{"scheduler", func(r *upscale.Request) { r.Scheduler = new("unknown") }},
		{"guidance", func(r *upscale.Request) { r.Guidance = new(math.NaN()) }},
		{"tile size", func(r *upscale.Request) { r.TileSize = new(513) }},
		{"tile overlap", func(r *upscale.Request) { r.TileOverlap = new(15) }},
		{"overlap exceeds size", func(r *upscale.Request) { r.TileSize = new(512); r.TileOverlap = new(512) }},
		{"empty selector", func(r *upscale.Request) { r.Components = &upscale.Components{TileControlNet: new("")} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := base
			test.change(&request)
			if err := upscale.ValidateRequest(request); err == nil {
				t.Fatal("invalid request was accepted")
			} else if _, ok := errors.AsType[*operation.InvalidRequestError](err); !ok {
				t.Fatalf("error = %T, want invalid request", err)
			}
		})
	}
}

func TestSubmitChecksSourceAndEnqueuesOnce(t *testing.T) {
	mutations := 0
	openAPI, err := os.ReadFile("../doctor/testdata/invokeai_6_14_anima_openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/app/version":
			_ = json.MarshalWrite(w, map[string]any{"version": "6.14.1"})
		case "/api/v2/models/":
			_ = json.MarshalWrite(w, map[string]any{"models": []map[string]any{
				{"key": "main", "hash": "main-hash", "name": "SDXL", "base": "sdxl", "type": "main", "variant": "normal"},
				{"key": "spandrel", "hash": "spandrel-hash", "name": "RealESRGAN", "base": "any", "type": "spandrel_image_to_image"},
				{"key": "controlnet", "hash": "controlnet-hash", "name": "Tile", "base": "sdxl", "type": "controlnet"},
			}})
		case "/openapi.json":
			_, _ = w.Write(openAPI)
		case "/api/v1/images/i/source.png":
			_ = json.MarshalWrite(w, map[string]any{
				"image_name": "source.png", "image_url": "/api/v1/images/i/source.png/full", "thumbnail_url": "/api/v1/images/i/source.png/thumbnail",
				"image_origin": "internal", "image_category": "general", "width": 513, "height": 513, "created_at": "2026-01-01", "updated_at": "2026-01-01",
			})
		case "/api/v1/queue/default/enqueue_batch":
			mutations++
			var payload struct {
				Batch struct {
					Graph struct {
						Nodes map[string]any `json:"nodes"`
					} `json:"graph"`
					Runs int `json:"runs"`
				} `json:"batch"`
			}
			if err := json.UnmarshalRead(r.Body, &payload); err != nil {
				t.Errorf("decode enqueue: %v", err)
			}
			if payload.Batch.Graph.Nodes["autoscale"] == nil || payload.Batch.Runs != 1 {
				t.Errorf("unexpected enqueue: %#v", payload)
			}
			_ = json.MarshalWrite(w, map[string]any{"queue_id": "default", "enqueued": 1, "requested": 1, "batch": map[string]any{"batch_id": "batch-2"}, "item_ids": []int{23}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for index, test := range []struct{ scale, expected int }{{2, 1024}, {4, 2048}, {8, 4104}} {
		request := upscale.Request{SchemaVersion: 1, Source: upscale.Source{Type: "image", Reference: "source.png"}, Model: "main", Scale: new(test.scale), Seed: new(uint32(42)), Components: &upscale.Components{TileControlNet: new("controlnet")}}
		receipt, err := upscale.Submit(t.Context(), client, request)
		if err != nil {
			t.Fatal(err)
		}
		if mutations != index+1 || receipt.ResolvedSettings.OutputWidth != test.expected || receipt.ResolvedSettings.OutputHeight != test.expected || receipt.ResolvedSettings.ComponentKeys["upscale_model"] != "spandrel" || receipt.Queue.ItemIDs[0] != 23 || len(receipt.Outputs) != 0 || receipt.SourceUploaded {
			t.Fatalf("scale %d: mutations=%d, receipt=%#v", test.scale, mutations, receipt)
		}
	}
}

func TestWaitRejectsCompletedImageWithWrongDimensions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/i/23":
			_ = json.MarshalWrite(w, map[string]any{
				"item_id": 23, "queue_id": "default", "batch_id": "batch-2", "session_id": "session-23", "status": "completed", "priority": 0,
				"created_at": "2026-01-01", "updated_at": "2026-01-01",
				"field_values": []map[string]any{{"node_path": "seed", "field_name": "value", "value": 42}},
				"session":      map[string]any{"results": map[string]any{"decode": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "output.png"}}}},
			})
		case "/api/v1/images/i/output.png":
			_ = json.MarshalWrite(w, map[string]any{
				"image_name": "output.png", "image_url": "/api/v1/images/i/output.png/full", "thumbnail_url": "/api/v1/images/i/output.png/thumbnail",
				"image_origin": "internal", "image_category": "general", "width": 1000, "height": 1024, "created_at": "2026-01-01", "updated_at": "2026-01-01",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	receipt := upscale.ExecutionReceipt{
		SourceImage:      images.Reference{ImageName: "source.png", Width: 513, Height: 513},
		ResolvedSettings: upscale.ResolvedSettings{Scale: 2, OutputWidth: 1024, OutputHeight: 1024, Seeds: []uint32{42}},
		Queue:            upscale.QueueReceipt{QueueID: "default", BatchID: "batch-2", ItemIDs: []int{23}},
	}
	result, err := upscale.Wait(t.Context(), client, receipt, upscale.WaitOptions{})
	failure, ok := errors.AsType[*upscale.ScaleNotAppliedError](err)
	if !ok || failure.ExpectedWidth != 1024 || failure.ActualWidth != 1000 || failure.SourceImage.ImageName != "source.png" || failure.Output.Image.ImageName != "output.png" || len(result.Outputs) != 0 {
		t.Fatalf("result = %#v, failure = %#v, err = %v", result, failure, err)
	}
}

func TestWaitSelectsOnlyFinalNonIntermediateUpscaleImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/queue/default/i/23":
			_ = json.MarshalWrite(w, map[string]any{
				"item_id": 23, "queue_id": "default", "batch_id": "batch-2", "session_id": "session-23", "status": "completed", "priority": 0,
				"created_at": "2026-01-01", "updated_at": "2026-01-01",
				"field_values": []map[string]any{{"node_path": "seed", "field_name": "value", "value": 42}},
				"session": map[string]any{"results": map[string]any{
					"autoscale": map[string]any{"type": "image_output", "image": map[string]any{"image_name": "autoscale.png"}},
					"unsharp":   map[string]any{"type": "image_output", "image": map[string]any{"image_name": "unsharp.png"}},
					"decode":    map[string]any{"type": "image_output", "image": map[string]any{"image_name": "final.png"}},
				}},
			})
		case "/api/v1/images/i/autoscale.png", "/api/v1/images/i/unsharp.png", "/api/v1/images/i/final.png":
			name := r.URL.Path[len("/api/v1/images/i/"):]
			_ = json.MarshalWrite(w, map[string]any{
				"image_name": name, "image_url": "/api/v1/images/i/" + name + "/full", "thumbnail_url": "/api/v1/images/i/" + name + "/thumbnail",
				"image_origin": "internal", "image_category": "general", "width": 1024, "height": 1024, "created_at": "2026-01-01", "updated_at": "2026-01-01",
				"is_intermediate": name != "final.png",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := httpclient.New(server.URL, "", httpclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	receipt := upscale.ExecutionReceipt{
		SourceImage:      images.Reference{ImageName: "source.png", Width: 512, Height: 512},
		ResolvedSettings: upscale.ResolvedSettings{Scale: 2, OutputWidth: 1024, OutputHeight: 1024, Seeds: []uint32{42}},
		Queue:            upscale.QueueReceipt{QueueID: "default", BatchID: "batch-2", ItemIDs: []int{23}},
	}
	got, err := upscale.Wait(t.Context(), client, receipt, upscale.WaitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Outputs) != 1 || got.Outputs[0].Image.ImageName != "final.png" || got.Outputs[0].Seed != 42 {
		t.Fatalf("outputs = %#v", got.Outputs)
	}
	_, strictErr := graphops.Wait(t.Context(), client, receipt.Queue, []uint32{42}, graphops.SeedField{NodePath: "seed", FieldName: "value"}, graphops.WaitOptions{})
	if _, ok := errors.AsType[*operation.InvalidQueueResultError](strictErr); !ok {
		t.Fatalf("generation-style strict wait accepted three image outputs: %v", strictErr)
	}
}

func TestWaitRejectsZeroOrMultipleFinalUpscaleOutputs(t *testing.T) {
	for _, test := range []struct {
		name         string
		results      map[string]string
		intermediate map[string]bool
	}{
		{"zero final", map[string]string{"autoscale": "a.png", "unsharp": "b.png"}, map[string]bool{"a.png": true, "b.png": true}},
		{"two final", map[string]string{"decode": "a.png", "other": "b.png"}, map[string]bool{"a.png": false, "b.png": false}},
		{"duplicate final reference", map[string]string{"decode": "a.png", "other": "a.png"}, map[string]bool{"a.png": false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/queue/default/i/23" {
					results := map[string]any{}
					for node, imageName := range test.results {
						results[node] = map[string]any{"type": "image_output", "image": map[string]any{"image_name": imageName}}
					}
					_ = json.MarshalWrite(w, map[string]any{
						"item_id": 23, "queue_id": "default", "batch_id": "batch-2", "session_id": "session-23", "status": "completed", "priority": 0,
						"created_at": "2026-01-01", "updated_at": "2026-01-01",
						"field_values": []map[string]any{{"node_path": "seed", "field_name": "value", "value": 42}},
						"session":      map[string]any{"results": results},
					})
					return
				}
				name := r.URL.Path[len("/api/v1/images/i/"):]
				intermediate, ok := test.intermediate[name]
				if !ok {
					http.NotFound(w, r)
					return
				}
				_ = json.MarshalWrite(w, map[string]any{
					"image_name": name, "image_url": "/api/v1/images/i/" + name + "/full", "thumbnail_url": "/api/v1/images/i/" + name + "/thumbnail",
					"image_origin": "internal", "image_category": "general", "width": 1024, "height": 1024, "created_at": "2026-01-01", "updated_at": "2026-01-01",
					"is_intermediate": intermediate,
				})
			}))
			defer server.Close()
			client, err := httpclient.New(server.URL, "", httpclient.Options{})
			if err != nil {
				t.Fatal(err)
			}
			receipt := upscale.ExecutionReceipt{
				SourceImage:      images.Reference{ImageName: "source.png", Width: 512, Height: 512},
				ResolvedSettings: upscale.ResolvedSettings{Scale: 2, OutputWidth: 1024, OutputHeight: 1024, Seeds: []uint32{42}},
				Queue:            upscale.QueueReceipt{QueueID: "default", BatchID: "batch-2", ItemIDs: []int{23}},
			}
			got, err := upscale.Wait(t.Context(), client, receipt, upscale.WaitOptions{})
			if _, ok := errors.AsType[*operation.InvalidQueueResultError](err); !ok || len(got.Outputs) != 0 {
				t.Fatalf("receipt=%#v error=%v", got, err)
			}
		})
	}
}

func TestCompileSDXLUsesStockTiledGraphAndFormulas(t *testing.T) {
	request := upscale.Request{SchemaVersion: 1, Source: upscale.Source{Type: "image", Reference: "source.png"}, Model: "main", Creativity: new(-10), Structure: new(10), Seed: new(uint32(42)), Components: &upscale.Components{TileControlNet: new("controlnet")}}
	inventory := []graphops.ModelIdentifier{
		{Key: "main", Name: "SDXL", Hash: "main-hash", Base: "sdxl", Type: "main", Variant: "normal"},
		{Key: "spandrel", Name: "RealESRGAN", Hash: "spandrel-hash", Base: "any", Type: "spandrel_image_to_image"},
		{Key: "controlnet", Name: "Tile", Hash: "controlnet-hash", Base: "sdxl", Type: "controlnet"},
	}
	resolved, err := upscale.Resolve(request, inventory, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	graphRequest, err := upscale.Compile(resolved, images.Reference{ImageName: "source.png", Width: 513, Height: 513})
	if err != nil {
		t.Fatal(err)
	}
	if graphRequest.Batch.Origin != "upscaling" || graphRequest.Batch.Destination != "gallery" || graphRequest.Batch.Runs != 1 {
		t.Fatalf("unexpected batch: %#v", graphRequest.Batch)
	}
	denoise := graphRequest.Batch.Graph.Nodes["denoise"].(map[string]any)
	if math.Abs(denoise["denoising_start"].(float64)-0.998) > 1e-12 || denoise["tile_width"] != 1024 || denoise["tile_overlap"] != 128 {
		t.Fatalf("denoiser = %#v", denoise)
	}
	first := graphRequest.Batch.Graph.Nodes["controlnet_1"].(map[string]any)
	second := graphRequest.Batch.Graph.Nodes["controlnet_2"].(map[string]any)
	if math.Abs(first["control_weight"].(float64)-0.95) > 1e-12 || math.Abs(first["end_step_percent"].(float64)-0.8) > 1e-12 || math.Abs(second["control_weight"].(float64)-0.36) > 1e-12 || math.Abs(second["begin_step_percent"].(float64)-0.8) > 1e-12 {
		t.Fatalf("controls = %#v, %#v", first, second)
	}
	if len(graphRequest.Batch.Data) != 2 || len(graphRequest.Batch.Data[0]) != 1 || graphRequest.Batch.Data[0][0].Items[0] != 42 || len(graphRequest.Batch.Data[1]) != 2 {
		t.Fatalf("seed batch = %#v", graphRequest.Batch.Data)
	}
	if _, ok := graphRequest.Batch.Graph.Nodes["positive_prompt"].(map[string]any)["value"]; ok {
		t.Fatal("stock positive prompt is supplied only through batch data")
	}
	if _, ok := graphRequest.Batch.Graph.Nodes["negative_prompt"].(map[string]any)["value"]; ok {
		t.Fatal("stock negative prompt is supplied only through batch data")
	}
	if _, ok := graphRequest.Batch.Graph.Nodes["seed"].(map[string]any)["value"]; ok {
		t.Fatal("stock seed is supplied only through batch data")
	}
	if _, ok := graphRequest.Batch.Graph.Nodes["metadata"].(map[string]any)["negative_prompt"]; ok {
		t.Fatal("stock negative metadata prompt comes from edge")
	}
	if len(graphRequest.Batch.Graph.Nodes) != 16 || len(graphRequest.Batch.Graph.Edges) != 32 {
		t.Fatalf("stock graph has 16 nodes and 32 edges; got %d nodes, %d edges", len(graphRequest.Batch.Graph.Nodes), len(graphRequest.Batch.Graph.Edges))
	}
	wantEdges := []graphops.Edge{
		graphops.Connect("autoscale", "image", "unsharp_mask", "image"),
		graphops.Connect("unsharp_mask", "width", "noise", "width"),
		graphops.Connect("unsharp_mask", "height", "noise", "height"),
		graphops.Connect("seed", "value", "noise", "seed"),
		graphops.Connect("unsharp_mask", "image", "encode", "image"),
		graphops.Connect("unsharp_mask", "image", "controlnet_1", "image"),
		graphops.Connect("unsharp_mask", "image", "controlnet_2", "image"),
		graphops.Connect("model_loader", "clip", "positive_conditioning", "clip"),
		graphops.Connect("model_loader", "clip2", "positive_conditioning", "clip2"),
		graphops.Connect("model_loader", "clip", "negative_conditioning", "clip"),
		graphops.Connect("model_loader", "clip2", "negative_conditioning", "clip2"),
		graphops.Connect("model_loader", "unet", "denoise", "unet"),
		graphops.Connect("positive_prompt", "value", "positive_conditioning", "prompt"),
		graphops.Connect("positive_prompt", "value", "positive_conditioning", "style"),
		graphops.Connect("negative_prompt", "value", "negative_conditioning", "prompt"),
		graphops.Connect("negative_prompt", "value", "negative_conditioning", "style"),
		graphops.Connect("noise", "noise", "denoise", "noise"),
		graphops.Connect("encode", "latents", "denoise", "latents"),
		graphops.Connect("positive_conditioning", "conditioning", "denoise", "positive_conditioning"),
		graphops.Connect("negative_conditioning", "conditioning", "denoise", "negative_conditioning"),
		graphops.Connect("denoise", "latents", "decode", "latents"),
		graphops.Connect("controlnet_1", "control", "control_collection", "item"),
		graphops.Connect("controlnet_2", "control", "control_collection", "item"),
		graphops.Connect("control_collection", "collection", "denoise", "control"),
		graphops.Connect("positive_prompt", "value", "metadata", "positive_prompt"),
		graphops.Connect("negative_prompt", "value", "metadata", "negative_prompt"),
		graphops.Connect("seed", "value", "metadata", "seed"),
		graphops.Connect("autoscale", "width", "metadata", "width"),
		graphops.Connect("autoscale", "height", "metadata", "height"),
		graphops.Connect("metadata", "metadata", "decode", "metadata"),
		graphops.Connect("model_loader", "vae", "encode", "vae"),
		graphops.Connect("model_loader", "vae", "decode", "vae"),
	}
	if !reflect.DeepEqual(graphRequest.Batch.Graph.Edges, wantEdges) {
		t.Fatalf("graph edges diverge from stock SDXL upscale topology")
	}
	resolved.Request.BoardID = "board-7"
	resolved.Models.VAE = graphops.ModelIdentifier{Key: "vae", Name: "Override", Hash: "vae-hash", Base: "sdxl", Type: "vae"}
	withVAE, err := upscale.Compile(resolved, images.Reference{ImageName: "source.png", Width: 513, Height: 513})
	if err != nil {
		t.Fatal(err)
	}
	if len(withVAE.Batch.Graph.Nodes) != 17 || len(withVAE.Batch.Graph.Edges) != 32 || !slices.Contains(withVAE.Batch.Graph.Edges, graphops.Connect("vae_loader", "vae", "decode", "vae")) || slices.Contains(withVAE.Batch.Graph.Edges, graphops.Connect("model_loader", "vae", "decode", "vae")) {
		t.Fatalf("VAE override wiring differs: %#v", withVAE.Batch.Graph.Edges)
	}
	decode := withVAE.Batch.Graph.Nodes["decode"].(map[string]any)
	if decode["board"] != (graphops.BoardField{BoardID: "board-7"}) {
		t.Fatalf("board = %#v", decode["board"])
	}
}

func TestResolveSDXLRequiresCallerToChooseTileControlNet(t *testing.T) {
	request := upscale.Request{SchemaVersion: 1, Source: upscale.Source{Type: "image", Reference: "source.png"}, Model: "main", Seed: new(uint32(7))}
	inventory := []graphops.ModelIdentifier{
		{Key: "main", Name: "SDXL", Hash: "main-hash", Base: "sdxl", Type: "main", Variant: "normal"},
		{Key: "spandrel", Name: "RealESRGAN", Hash: "spandrel-hash", Base: "any", Type: "spandrel_image_to_image"},
		{Key: "controlnet", Name: "Tile", Hash: "controlnet-hash", Base: "sdxl", Type: "controlnet"},
	}
	_, err := upscale.Resolve(request, inventory, bytes.NewReader(nil))
	selection, ok := errors.AsType[*operation.SelectionRequiredError](err)
	if !ok || selection.Kind != "tile_controlnet" || selection.Selector != "" || len(selection.Candidates) != 1 || selection.Candidates[0].Key != "controlnet" {
		t.Fatalf("selection = %#v, error = %v", selection, err)
	}
	request.Components = &upscale.Components{TileControlNet: new("controlnet")}
	resolved, err := upscale.Resolve(request, inventory, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if *resolved.Request.Scale != 4 || *resolved.Request.Steps != 30 || *resolved.Request.Scheduler != "kdpm_2" || *resolved.Request.Guidance != 2 || *resolved.Request.TileSize != 1024 || *resolved.Request.TileOverlap != 128 || resolved.Models.UpscaleModel.Key != "spandrel" {
		t.Fatalf("unexpected resolution: %#v", resolved)
	}
}

func TestResolveSDXLReportsComponentChoicesAndUnsupportedMain(t *testing.T) {
	main := graphops.ModelIdentifier{Key: "main", Name: "SDXL", Hash: "main-hash", Base: "sdxl", Type: "main", Variant: "normal"}
	spandrelA := graphops.ModelIdentifier{Key: "spandrel-a", Name: "Upscaler A", Hash: "a", Base: "any", Type: "spandrel_image_to_image"}
	spandrelB := graphops.ModelIdentifier{Key: "spandrel-b", Name: "Upscaler B", Hash: "b", Base: "any", Type: "spandrel_image_to_image"}
	tile := graphops.ModelIdentifier{Key: "tile", Name: "Tile", Hash: "tile-hash", Base: "sdxl", Type: "controlnet"}
	request := upscale.Request{SchemaVersion: 1, Source: upscale.Source{Type: "image", Reference: "source.png"}, Model: "main", Seed: new(uint32(7)), Components: &upscale.Components{TileControlNet: new("tile")}}
	_, err := upscale.Resolve(request, []graphops.ModelIdentifier{main, spandrelB, tile, spandrelA}, bytes.NewReader(nil))
	choice, ok := errors.AsType[*operation.SelectionRequiredError](err)
	if !ok || choice.Kind != "upscale_model" || len(choice.Candidates) != 2 || choice.Candidates[0].Key != "spandrel-a" || choice.Candidates[1].Key != "spandrel-b" {
		t.Fatalf("ambiguous Spandrel choice = %#v, error = %v", choice, err)
	}
	_, err = upscale.Resolve(request, []graphops.ModelIdentifier{main, tile}, bytes.NewReader(nil))
	missing, ok := errors.AsType[*operation.MissingComponentError](err)
	if !ok || missing.ComponentType != "upscale_model" || missing.InstallationGuidance != "install the RealESRGAN_x4plus starter" {
		t.Fatalf("missing Spandrel = %#v, error = %v", missing, err)
	}
	request.Components.UpscaleModel = new("spandrel-a")
	wrongBase := main
	wrongBase.Base = "sd-2"
	_, err = upscale.Resolve(request, []graphops.ModelIdentifier{wrongBase, spandrelA, tile}, bytes.NewReader(nil))
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok {
		t.Fatalf("wrong main base error = %v", err)
	}
	wrongVariant := main
	wrongVariant.Variant = "inpaint"
	_, err = upscale.Resolve(request, []graphops.ModelIdentifier{wrongVariant, spandrelA, tile}, bytes.NewReader(nil))
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok {
		t.Fatalf("wrong main variant error = %v", err)
	}
	request.Components.TileControlNet = new("other-base")
	wrongTile := tile
	wrongTile.Key = "other-base"
	wrongTile.Base = "sd-1"
	_, err = upscale.Resolve(request, []graphops.ModelIdentifier{main, spandrelA, wrongTile}, bytes.NewReader(nil))
	if _, ok := errors.AsType[*operation.UnsupportedCapabilityError](err); !ok {
		t.Fatalf("wrong tile base error = %v", err)
	}
}
