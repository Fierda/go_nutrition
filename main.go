package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	_ "fierda/go_nutrition/docs"
)


type Entry struct {
	ID        int                 `json:"id" example:"1"`
	Date      string              `json:"date" example:"2025-08-11"`
	Query     string              `json:"query" example:"1 cup rice"`
	Nutrients NutritionixResponse `json:"nutrients"`
	CreatedAt time.Time           `json:"created_at" example:"2025-08-11T10:00:00Z"`
}

type NutritionixResponse struct {
	Foods []Food `json:"foods"`
}

type Food struct {
	FoodName       string  `json:"food_name" example:"rice"`
	ServingQty     float64 `json:"serving_qty" example:"1"`
	ServingUnit    string  `json:"serving_unit" example:"cup"`
	ServingWeight  float64 `json:"serving_weight_grams" example:"158"`
	NFCalories     float64 `json:"nf_calories" example:"205.4"`
	NFProtein      float64 `json:"nf_protein" example:"4.25"`
	NFTotalFat     float64 `json:"nf_total_fat" example:"0.44"`
	NFTotalCarbs   float64 `json:"nf_total_carbohydrate" example:"44.51"`
	NFSodium       float64 `json:"nf_sodium" example:"1.58"`
	NFSugars       float64 `json:"nf_sugars" example:"0.08"`
	NFDietaryFiber float64 `json:"nf_dietary_fiber" example:"0.63"`
	Photo          Photo   `json:"photo"`
}

type Photo struct {
	Thumb   string `json:"thumb" example:"https://nix-tag-images.s3.amazonaws.com/784_thumb.jpg"`
	Highres string `json:"highres" example:"https://nix-tag-images.s3.amazonaws.com/784_highres.jpg"`
}

// SimplifiedEntry represents a simplified nutrition entry response
type SimplifiedEntry struct {
	ID          int       `json:"id" example:"1"`
	Date        string    `json:"date" example:"2025-08-11"`
	Query       string    `json:"query" example:"1 cup rice"`
	FoodName    string    `json:"food_name" example:"rice"`
	ServingSize string    `json:"serving_size" example:"1.0 cup"`
	Calories    float64   `json:"calories" example:"205.4"`
	Protein     float64   `json:"protein_g" example:"4.25"`
	Carbs       float64   `json:"carbs_g" example:"44.51"`
	Fat         float64   `json:"fat_g" example:"0.44"`
	ImageURL    string    `json:"image_url,omitempty" example:"https://nix-tag-images.s3.amazonaws.com/784_thumb.jpg"`
	CreatedAt   time.Time `json:"created_at" example:"2025-08-11T10:00:00Z"`
}

// CreateEntryRequest represents the request body for creating an entry
type CreateEntryRequest struct {
	Query string `json:"query" binding:"required" example:"1 cup rice" minLength:"1"`
	Date  string `json:"date" binding:"required" example:"2025-08-11" format:"date"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error" example:"Entry not found"`
}

// HealthResponse represents health check response
type HealthResponse struct {
	Status    string    `json:"status" example:"healthy"`
	Entries   int       `json:"entries" example:"5"`
	Timestamp time.Time `json:"timestamp" example:"2025-08-11T10:00:00Z"`
}

// In-Memory Storage
var (
	mu     sync.RWMutex
	store  = make(map[int]Entry)
	nextID = 1
	appID  string
	appKey string
)

// API Client

func fetchNutrients(query string) (NutritionixResponse, error) {
	reqBody, _ := json.Marshal(map[string]string{"query": query})
	
	req, err := http.NewRequest("POST", "https://trackapi.nutritionix.com/v2/natural/nutrients", bytes.NewBuffer(reqBody))
	if err != nil {
		return NutritionixResponse{}, err
	}
	
	req.Header.Set("x-app-id", appID)
	req.Header.Set("x-app-key", appKey)
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return NutritionixResponse{}, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return NutritionixResponse{}, fmt.Errorf("nutritionix API error: status %d", resp.StatusCode)
	}
	
	var nutriResp NutritionixResponse
	if err := json.NewDecoder(resp.Body).Decode(&nutriResp); err != nil {
		return NutritionixResponse{}, err
	}
	
	return nutriResp, nil
}

// ===== HANDLERS =====

// GetEntries godoc
// @Summary Get all nutrition entries
// @Description Get all nutrition entries with optional simplified format
// @Tags entries
// @Accept json
// @Produce json
// @Param format query string false "Response format (simple)" Enums(simple)
// @Success 200 {array} Entry "Full format entries"
// @Success 200 {array} SimplifiedEntry "Simplified format entries (when format=simple)"
// @Router /entries [get]
func getEntries(c *gin.Context) {
	format := c.Query("format")
	
	mu.RLock()
	entries := make([]Entry, 0, len(store))
	for _, entry := range store {
		entries = append(entries, entry)
	}
	mu.RUnlock()
	
	if format == "simple" {
		simplified := make([]SimplifiedEntry, len(entries))
		for i, entry := range entries {
			simplified[i] = toSimplified(entry)
		}
		c.JSON(http.StatusOK, simplified)
		return
	}
	
	c.JSON(http.StatusOK, entries)
}

// GetEntryByID godoc
// @Summary Get nutrition entry by ID
// @Description Get a specific nutrition entry by its ID
// @Tags entries
// @Accept json
// @Produce json
// @Param id path int true "Entry ID"
// @Success 200 {object} Entry
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /entries/{id} [get]
func getEntryByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID format"})
		return
	}
	
	mu.RLock()
	entry, exists := store[id]
	mu.RUnlock()
	
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Entry not found"})
		return
	}
	
	c.JSON(http.StatusOK, entry)
}

// CreateEntry godoc
// @Summary Create new nutrition entry
// @Description Create a new nutrition entry by querying Nutritionix API
// @Tags entries
// @Accept json
// @Produce json
// @Param entry body CreateEntryRequest true "Entry data"
// @Success 201 {object} Entry
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /entries [post]
func createEntry(c *gin.Context) {
	var req CreateEntryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	// Fetch from Nutritionix
	nutrients, err := fetchNutrients(req.Query)
	if err != nil {
		log.Printf("Nutritionix API error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch nutrition data"})
		return
	}
	
	// Store in memory
	mu.Lock()
	entry := Entry{
		ID:        nextID,
		Date:      req.Date,
		Query:     req.Query,
		Nutrients: nutrients,
		CreatedAt: time.Now(),
	}
	store[nextID] = entry
	nextID++
	mu.Unlock()
	
	c.JSON(http.StatusCreated, entry)
}

// Simplification

func toSimplified(entry Entry) SimplifiedEntry {
	simplified := SimplifiedEntry{
		ID:        entry.ID,
		Date:      entry.Date,
		Query:     entry.Query,
		CreatedAt: entry.CreatedAt,
	}
	
	if len(entry.Nutrients.Foods) > 0 {

		var totalCalories, totalProtein, totalCarbs, totalFat float64
		var foodNames []string
		var servingSizes []string
		var imageURL string

		for _, food := range entry.Nutrients.Foods {
			totalCalories += food.NFCalories
			totalProtein += food.NFProtein
			totalCarbs += food.NFTotalCarbs
			totalFat += food.NFTotalFat
			foodNames = append(foodNames, food.FoodName)
			servingSizes = append(servingSizes, fmt.Sprintf("%.1f %s", food.ServingQty, food.ServingUnit))
			
			if imageURL == "" && food.Photo.Thumb != "" {
				imageURL = food.Photo.Thumb
			}
		}

		
		simplified.FoodName = strings.Join(foodNames, " + ")
		simplified.ServingSize = strings.Join(servingSizes, " + ")
		simplified.Calories = totalCalories
		simplified.Protein = totalProtein
		simplified.Carbs = totalCarbs
		simplified.Fat = totalFat
		simplified.ImageURL = imageURL
	}
	
	return simplified
}

func loadConfig() error {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: No .env file found")
	}
	
	appID = os.Getenv("APP_ID")
	appKey = os.Getenv("APP_KEY")
	
	if appID == "" || appKey == "" {
		return fmt.Errorf("missing required environment variables: APP_ID and APP_KEY")
	}
	
	return nil
}

// ===== MAIN =====

// @title Nutrition Tracker API
// @version 1.0
// @description A simple nutrition tracking API using Nutritionix integration on Gin Framework
// @termsOfService http://swagger.io/terms/

// @license.name MIT
// @license.url https://opensource.org/licenses/MIT

// @host localhost:9000
// @BasePath /
// @schemes http
func main() {
	// Load config
	if err := loadConfig(); err != nil {
		log.Fatal(err)
	}
	
	// Setup Gin
	r := gin.Default()
	
	// Middleware
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	
	// Swagger endpoint
	r.GET("/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	
	// Routes
	r.GET("/entries", getEntries)           // ?format=simple for clean response
	r.GET("/entries/:id", getEntryByID)
	r.POST("/entries", createEntry)
	
	// Health check
	// @Summary Health check
	// @Description Check if the API is running
	// @Tags health
	// @Produce json
	// @Success 200 {object} HealthResponse
	// @Router /health [get]
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, HealthResponse{
			Status:    "healthy",
			Entries:   len(store),
			Timestamp: time.Now(),
		})
	})
	
	log.Println("Server starting on :9000")
	log.Println("📚 Swagger docs available at: http://localhost:9000/docs/index.html")
	
	if err := r.Run(":9000"); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}