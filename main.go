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
	"strings"
	"time"
)

// 746 токенов на 10 товаров 894 891 в месяц ~ 11_995

type ChatClient struct {
	client    pb.ChatServiceClient
	ctx       context.Context
	args      params.Params
	expiresAt time.Time
}

func (chat *ChatClient) Close() {
	if closer, ok := chat.client.(io.Closer); ok {
		closer.Close()
	}
}

// NewChatClient создает новый экземпляр ChatClient
func NewChatClient(args params.Params) *ChatClient {
	// Устанавливаем соединение с gRPC сервером
	creds := credentials.NewClientTLSFromCert(nil, "")
	conn, err := grpc.NewClient("gigachat.devices.sberbank.ru", grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatalf("%v", err)
	}
	// Создаем контекст с тайм-аутом
	ctx := context.Background()
	return &ChatClient{
		client:    pb.NewChatServiceClient(conn),
		ctx:       ctx,
		args:      args,
		expiresAt: time.Now().Add(-time.Hour),
	}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	args := params.LoadParams()
	//Устанавливаем соединение с gRPC сервером
	chat := NewChatClient(args)
	api := ozon.NewApi(args)
	reviews := loadNewReviews(api)
	idsToQuarantine := make(map[string]bool, len(reviews)) // no sets in go
	for _, review := range reviews {
		fullReview := strings.Join([]string{review.Text.Negative, review.Text.Positive, review.Text.Comment}, " ")
		isNegative, err := chat.isNegativeResponse(fullReview)
		if err != nil {
			log.Printf("ошибка при обработке отзыва %v %v", review, err)
		}
		if isNegative {
			log.Printf("%v добавлен в список для обновления цен", review)
			idsToQuarantine[review.Product.OfferID] = true
		} else {
			log.Printf("%v является положительным отзывом", review)
		}
	}
	offerIds := make([]string, len(idsToQuarantine))
	for id := range idsToQuarantine {
		offerIds = append(offerIds, id)
	}
	chunkSize := 1000
	for i := 0; i < len(offerIds); i += chunkSize {
		end := min(i+chunkSize, len(offerIds))
		if api.PlaceToQuarantine(offerIds[i:end]) != nil {
			log.Printf("ошибка при обновлении цен %v", offerIds[i:end])
		}
	}
}

// TODO better write after all processing is done. save last processed time at the and of main. or take the time of last successful price change
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

func (chat *ChatClient) isNegativeResponse(userResponse string) (bool, error) {
	chat.updateAccessTokenIfNecessary()
	request := &pb.ChatRequest{
		Model: "GigaChat",
		Messages: []*pb.Message{
			{
				Role:    "system",
				Content: chat.args[params.GIGACHAT_PROMPT],
			},
			{
				Role:    "user",
				Content: userResponse,
			},
		},
	}
	response, err := chat.client.Chat(chat.ctx, request)
	if err != nil {
		return false, fmt.Errorf("Ошибка при вызове метода Chat: %v", err)
	}
	if len(response.Alternatives) < 1 {
		return false, fmt.Errorf("не получен ответ от модели")
	}
	class := response.Alternatives[0].Message.Content
	log.Printf("prompt:%s\nотзыв '%s' ответы: %v", chat.args[params.GIGACHAT_PROMPT], userResponse, response.Alternatives)
	return class == "отрицательный", nil
}

type AuthResponse struct {
	AccessToken string `json:"access_token"` // Токен доступа
	ExpiresAt   int64  `json:"expires_at"`   // Время истечения токена в миллисекундах
}

func (chat *ChatClient) updateAccessTokenIfNecessary() {
	if time.Now().Before(chat.expiresAt.Add(-time.Minute)) {
		return
	}
	log.Println("обновляю access токен")
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
	req.Header.Set("Authorization", fmt.Sprintf("Basic %s", chat.args[params.GIGACHAT_AUTH_DATA]))
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
	chat.expiresAt = time.UnixMilli(authResponse.ExpiresAt)
	md := metadata.Pairs("Authorization", "Bearer "+authResponse.AccessToken)
	chat.ctx = metadata.NewOutgoingContext(chat.ctx, md)
}
