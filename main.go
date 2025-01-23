package main

import (
	pb "PriceGuardian/gigachat"
	"PriceGuardian/ozon"
	"PriceGuardian/params"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	uuid "github.com/nu7hatch/gouuid"
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
	"strings"
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
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	args := params.LoadParams()
	// Устанавливаем соединение с gRPC сервером
	chat := NewChatClient(getAccessToken(args))
	api := ozon.NewApi(args)
	next := true
	maxDiff, err := time.ParseDuration(args[params.TIME_DEPTH])
	if err != nil {
		log.Fatal(err)
	}
	start := time.Now()
	end := start
	for next && start.Sub(end) <= maxDiff {
		res := api.GetNextChunk()
		for i, rev := range res.Result {
			fullReview := strings.Join([]string{rev.Text.Negative, rev.Text.Positive, rev.Text.Comment}, " ")
			negative := isNegativeResponse(chat, fullReview, args)
			log.Printf("%d\nотзыв: %v\nнегативный: %t\n", i+1, rev, negative)
			if negative {
				log.Println("process...")
			}
		}
		next = res.HasNext
		tt, _ := strconv.ParseInt(res.PaginationLastTimestamp, 10, 64)
		end = time.UnixMicro(tt)
	}
	rev := loadNewReviews(api)
	for _, r := range rev {
		log.Println(r)
	}
	log.Println("-----------------------------")
	rev = loadNewReviews(api)
	for _, r := range rev {
		log.Println(r)
	}
}

func loadNewReviews(api *ozon.Api) []*ozon.Review {
	start, err := os.ReadFile("start.txt")
	if err != nil {
		log.Fatalf("failed to read start time from file")
	}
	t, _ := time.Parse(time.RFC3339Nano, string(start))
	reviews, nextStart := api.GetReviewsTillTime(t)
	if err := os.WriteFile("start.txt", []byte(nextStart.Format(time.RFC3339Nano)), 0644); err != nil {
		log.Fatal("failed to save next start time in file")
	}
	return reviews
}

func isNegativeResponse(chat *ChatClient, userResponse string, args params.Params) bool {
	request := &pb.ChatRequest{
		Model: "GigaChat",
		Messages: []*pb.Message{
			{
				Role:    "system",
				Content: args[params.PROMPT],
			},
			{
				Role:    "user",
				Content: userResponse,
			},
		},
	}
	response, err := chat.client.Chat(chat.ctx, request)
	if err != nil {
		log.Printf("Ошибка при вызове метода Chat: %v", err)
		return false
	}
	if len(response.Alternatives) < 1 {
		log.Printf("не получен ответ от модели")
		return false
	}
	class := response.Alternatives[0].Message.Content
	return class == "отрицательный"
}

type AuthResponse struct {
	AccessToken string `json:"access_token"` // Токен доступа
	ExpiresAt   int64  `json:"expires_at"`   // Время истечения токена в миллисекундах
}

func getAccessToken(args params.Params) string {
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
	id, _ := uuid.NewV4()
	req.Header.Set("RqUID", id.String())
	req.Header.Set("Authorization", fmt.Sprintf("Basic %s", args[params.GIGACHAT_AUTH_DATA]))
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
	return authResponse.AccessToken // TODO add logic to refresh token
}
