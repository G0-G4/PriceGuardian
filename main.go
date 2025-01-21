package main

import (
	pb "PriceGuardian/gigachat"
	"PriceGuardian/ozon"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

// 746 токенов на 10 товаров 894 891 в месяц ~ 11_995

type ChatClient struct {
	client pb.ChatServiceClient
	ctx    context.Context
}

func (c *ChatClient) Close() {
	if closer, ok := c.client.(io.Closer); ok {
		closer.Close()
	}
}

// NewChatClient создает новый экземпляр ChatClient
func NewChatClient(token string) *ChatClient {
	// Устанавливаем соединение с gRPC сервером
	creds := credentials.NewClientTLSFromCert(nil, "")
	conn, err := grpc.NewClient("gigachat.devices.sberbank.ru", grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatalf("%v", err)
	}

	// Создаем контекст с тайм-аутом
	ctx := context.Background()

	// Добавляем токен аутентификации в метаданные
	md := metadata.Pairs("Authorization", "Bearer "+token)
	ctx = metadata.NewOutgoingContext(ctx, md)

	return &ChatClient{
		client: pb.NewChatServiceClient(conn),
		ctx:    ctx,
	}
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("No .env file found")
	}
	// Устанавливаем соединение с gRPC сервером
	chat := NewChatClient(getAccessToken())
	api := ozon.NewApi()
	next := true
	maxDiff, err := time.ParseDuration("24h")
	if err != nil {
		log.Fatal(err)
	}
	start := time.Now()
	end := start
	for next && start.Sub(end) <= maxDiff {
		res := ozon.GetChunk(api)
		for i, rev := range res.Result {
			fullReview := rev.Text.Negative + rev.Text.Positive + rev.Text.Comment
			negative := isNegativeResponse(chat, fullReview)
			fmt.Printf("%v\n", rev)
			fmt.Printf("%d, отзыв: %v\nнегативный:%t\n----\n", i+1, rev, negative)
		}
		next = res.HasNext
		tt, _ := strconv.ParseInt(res.PaginationLastTimestamp, 10, 64)
		end = time.UnixMicro(tt)
		fmt.Printf("%v, %v\n", end, start.Sub(end))
	}
}

func isNegativeResponse(chat *ChatClient, userResponse string) bool {
	//TODO do not stop program if one checking fails
	request := &pb.ChatRequest{
		Model: "GigaChat",
		Messages: []*pb.Message{
			{
				Role: "system",
				Content: `Классифицируй обращения пользователя в подходящую категорию.
				Категории: положительный_отзыв, отрицательный_отзыв. В ответе укажи только категорию.`,
			},
			{
				Role:    "user",
				Content: userResponse,
			},
		},
	}
	response, err := chat.client.Chat(chat.ctx, request)
	if err != nil {
		log.Fatalf("Ошибка при вызове метода Chat: %v", err)
	}

	if len(response.Alternatives) < 1 {
		log.Fatalf("не получен ответ от модели")
	}
	class := response.Alternatives[0].Message.Content
	log.Printf("Ответ от модели: %s", class)
	return class == "отрицательный_отзыв"
}

type AuthResponse struct {
	AccessToken string `json:"access_token"` // Токен доступа
	ExpiresAt   int64  `json:"expires_at"`   // Время истечения токена в миллисекундах
}

func getAccessToken() string {
	// URL для запроса
	apiURL := "https://ngw.devices.sberbank.ru:9443/api/v2/oauth"

	// Создаем данные для запроса
	data := url.Values{}
	data.Set("scope", "GIGACHAT_API_PERS")

	// Создаем новый HTTP-запрос
	req, err := http.NewRequest("POST", apiURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		log.Fatalf("Ошибка при создании запроса: %v", err)
	}

	// Устанавливаем заголовки
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("RqUID", "d04d593f-f9f2-4a28-a742-17653bf96d0e")
	gigachatAuthData, exists := os.LookupEnv("GIGACHAT_AUTH_DATA")
	if !exists {
		log.Fatal("задайте GIGACHAT_AUTH_DATA")
	}
	req.Header.Set("Authorization", fmt.Sprintf("Basic %s", gigachatAuthData))

	// Отправляем запрос
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Ошибка при отправке запроса: %v", err)
	}
	defer resp.Body.Close()

	// Проверяем статус-код ответа
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Ошибка: статус-код %d %v", resp.StatusCode, err)
	}

	var authResponse AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResponse); err != nil {
		log.Fatalf("Ошибка декодирования %v", err)
	}
	return authResponse.AccessToken
}
