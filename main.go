package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/spaolacci/murmur3"
	"go-localization-large-backend/pkg/model"
)

// Payload holds a loaded payload file's name and raw JSON content.
type Payload struct {
	Name string
	Data json.RawMessage
}

var payloads []Payload

func init() {
	entries, err := os.ReadDir("payloads")
	if err != nil {
		log.Fatalf("failed to read payloads directory: %v", err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join("payloads", e.Name()))
		if err != nil {
			log.Printf("warning: skipping %s: %v", e.Name(), err)
			continue
		}
		payloads = append(payloads, Payload{Name: e.Name(), Data: data})
		log.Printf("loaded payload %s (%d bytes)", e.Name(), len(data))
	}

	if len(payloads) == 0 {
		log.Fatal("no payloads found in payloads/ directory")
	}
	log.Printf("loaded %d payloads", len(payloads))
}

// CurrentExperimentID is included in the hash key to prevent cross-experiment
// contamination. Without it, the same users always land in the same buckets
// across different experiments, which biases results. Change this value
// when launching a new experiment.
const CurrentExperimentID = "localization-ab-v1"

// assignPayload uses MurmurHash3 + modulo to deterministically assign a user
// to one of the payloads.
func assignPayload(userID string) *Payload {
	hash := murmur3.Sum32([]byte(userID + ":" + CurrentExperimentID))
	bucket := hash % uint32(len(payloads))
	return &payloads[bucket]
}

func main() {
	// Create a new Fiber instance
	app := fiber.New(fiber.Config{
		AppName: "Go Localization Backend",
	})

	// Middleware
	app.Use(logger.New())
	app.Use(recover.New())

	// Health check endpoint
	app.Get("/health", healthCheck)

	// Experiment endpoint
	app.Post("/experiment", experiment)

	// Start server
	log.Fatal(app.Listen(":3000"))
}

// Health check handler
func healthCheck(c *fiber.Ctx) error {
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"status":  "ok",
		"message": "Server is running",
	})
}

// Experiment handler
func experiment(c *fiber.Ctx) error {
	var req model.Request
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	if req.UserID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "userId is required",
		})
	}

	p := assignPayload(req.UserID)

	resp := model.Response{
		ExperimentID:        fmt.Sprintf("exp-%d", len(payloads)),
		SelectedPayloadName: p.Name,
		Payload:             string(p.Data),
	}

	return c.Status(fiber.StatusOK).JSON(resp)
}
