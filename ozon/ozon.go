package ozon

import (
	"PriceGuardian/params"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

type ReviewText struct {
	Positive string `json:"positive"`
	Negative string `json:"negative"`
	Comment  string `json:"comment"`
}

func (text ReviewText) String() string {
	return strings.Join([]string{text.Positive, text.Negative, text.Comment}, " ")
}

type Product struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	OfferID     string `json:"offer_id"`
	CompanyID   string `json:"company_info.id"`
	CompanyName string `json:"company_info.name"`
	BrandID     string `json:"brand_info.id"`
	BrandName   string `json:"brand_info.name"`
	CoverImage  string `json:"cover_image"`
}

type Review struct {
	ID                string     `json:"id"`
	SKU               string     `json:"sku"`
	Text              ReviewText `json:"text"`
	PublishedAt       string     `json:"published_at"`
	Rating            int        `json:"rating"`
	AuthorName        string     `json:"author_name"`
	Product           Product    `json:"product"`
	UUID              string     `json:"uuid"`
	OrderDeliveryType string     `json:"orderDeliveryType"`
	IsPinned          bool       `json:"is_pinned"`
}

func (review Review) String() string {
	return fmt.Sprintf("{%s %s %v %s}", review.ID, review.SKU, review.Text, review.PublishedAt)
}

type ReviewsList struct {
	Result                  []Review `json:"result"`
	HasNext                 bool     `json:"hasNext"`
	PaginationLastTimestamp string   `json:"pagination_last_timestamp"`
	PaginationLastUUID      string   `json:"pagination_last_uuid"`
}

type loadParams struct {
	PaginationLastTimestamp *string                `json:"pagination_last_timestamp"`
	PaginationLastUuid      *string                `json:"pagination_last_uuid"`
	WithCounters            bool                   `json:"with_counters"`
	Sort                    map[string]string      `json:"sort"`
	CompanyType             string                 `json:"company_type"`
	Filter                  map[string]interface{} `json:"filter"`
	CompanyId               string                 `json:"company_id"`
}

type cookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PriceChangeResponse struct {
	Result []struct {
		ProductID int     `json:"product_id"`
		OfferID   string  `json:"offer_id"`
		Updated   bool    `json:"updated"`
		Errors    []Error `json:"errors"`
	} `json:"result"`
}

type Price struct {
	Price                float64 `json:"price"`
	OldPrice             float64 `json:"old_price"`
	MinPrice             float64 `json:"min_price"`
	MarketingPrice       float64 `json:"marketing_price"`
	MarketingSellerPrice float64 `json:"marketing_seller_price"`
	RetailPrice          float64 `json:"retail_price"`
	VAT                  float64 `json:"vat"`
	CurrencyCode         string  `json:"currency_code"`
	AutoActionEnabled    bool    `json:"auto_action_enabled"`
}
type Item struct {
	ProductID int    `json:"product_id"`
	OfferID   string `json:"offer_id"`
	Price     Price  `json:"price"`
}

type PriceResponse struct {
	Cursor string `json:"cursor"`
	Items  []Item `json:"items"`
	Total  int32  `json:"total"`
}

type Api struct {
	session           *http.Client
	url               string
	answerURL         string
	commentAnswersURL string
	companyID         string
	loadMoreParams    loadParams
	headers           map[string]string
	arguments         params.Params
}

func NewApi(args params.Params) *Api {
	jar, _ := cookiejar.New(nil)
	api := &Api{
		session: &http.Client{
			Jar: jar,
		},
		url:               "https://seller.ozon.ru/api/v3/review/list",
		answerURL:         "https://seller.ozon.ru/api/review/comment/create",
		commentAnswersURL: "https://seller.ozon.ru/api/review/comment/list",
		companyID:         args[params.COMPANY_ID],
		loadMoreParams: loadParams{
			WithCounters:            false,
			Sort:                    map[string]string{"sort_by": "PUBLISHED_AT", "sort_direction": "DESC"},
			CompanyType:             "seller",
			Filter:                  map[string]interface{}{"interaction_status": []string{"NOT_VIEWED"}},
			CompanyId:               args[params.COMPANY_ID],
			PaginationLastTimestamp: nil,
			PaginationLastUuid:      nil,
		},
		headers: map[string]string{
			"authority":       "seller.ozon.ru",
			"accept":          "application/json, text/plain, */*",
			"accept-language": "ru",
			"content-type":    "application/json",
			"origin":          "https://seller.ozon.ru",
			"referer":         "https://seller.ozon.ru/app/reviews",
			"user-agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
			"x-o3-app-name":   "seller-ui",
			"x-o3-company-id": args[params.COMPANY_ID],
			"x-o3-language":   "ru",
			"x-o3-page-type":  "review",
		},
		arguments: args,
	}
	api.loadCookies()
	return api
}

func (api *Api) loadCookies() {
	plan, err := os.ReadFile(api.arguments[params.COOKIES_PATH])
	if err != nil {
		log.Fatalf("не удалось прочитать куки файл %v", err)
	}
	var data []cookie
	err = json.Unmarshal(plan, &data)
	if err != nil {
		log.Fatalf("не удалось распарсить куки файл, %v", err)
	}
	cookies := make([]*http.Cookie, 0, 15)
	for _, c := range data {
		cookie := &http.Cookie{
			Name:  c.Name,
			Value: c.Value,
		}
		cookies = append(cookies, cookie)
	}
	base, err := url.Parse("https://seller.ozon.ru/api/")
	if err != nil {
		log.Fatal(err)
	}
	api.session.Jar.SetCookies(base, cookies)
}

func (api *Api) GetReviewsTillTime(startTime time.Time) ([]*Review, time.Time) {
	reviews := make([]*Review, 0, 100)
	next := true
	var nextStart *time.Time = nil
	brk := true
	for next && brk {
		res, err := api.GetNextChunk()
		if err != nil {
			log.Printf("ошибка при получении страницы товаров %v. отзывы начиная с %v до последней успешно полученной страницы не будут обработаны",
				err, startTime)
			break
		}
		for _, rev := range res.Result {
			t, _ := time.Parse(time.RFC3339Nano, rev.PublishedAt)
			if nextStart == nil && t.After(startTime) {
				nextStart = &t
			}
			if t.Before(startTime) || t.Equal(startTime) {
				brk = false
				break
			}
			reviews = append(reviews, &rev)
		}
		next = res.HasNext
	}
	api.loadMoreParams.PaginationLastUuid = nil
	api.loadMoreParams.PaginationLastTimestamp = nil
	if nextStart == nil {
		return reviews, startTime
	}
	return reviews, *nextStart
}

func (api *Api) GetNextChunk() (ReviewsList, error) {
	loadMoreParams, err := json.Marshal(api.loadMoreParams)
	if err != nil {
		return ReviewsList{}, fmt.Errorf("не удалось подготовить параметры для загрузки товаров %v", err)
	}
	req, err := http.NewRequest("POST", "https://seller.ozon.ru/api/v3/review/list", bytes.NewBuffer(loadMoreParams))
	if err != nil {
		return ReviewsList{}, fmt.Errorf("ошибка при подготовке запроса %v", err)
	}
	for k, v := range api.headers {
		req.Header.Set(k, v)
	}
	resp, err := api.session.Do(req)
	if err != nil {
		return ReviewsList{}, fmt.Errorf("ошибка при получении списка товаров: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode > 299 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyString := string(bodyBytes)
		return ReviewsList{}, fmt.Errorf("запрос завершился со кодом %d: %s", resp.StatusCode, bodyString)
	}
	var res ReviewsList
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return ReviewsList{}, fmt.Errorf("ошибка декодирования %v", err)
	}
	api.loadMoreParams.PaginationLastUuid = &res.PaginationLastUUID
	api.loadMoreParams.PaginationLastTimestamp = &res.PaginationLastTimestamp
	log.Printf("успешно получена страница с параметрами %v %v", res.PaginationLastUUID, res.PaginationLastTimestamp)
	return res, nil
}

func (api *Api) GetPrice(offerIDs []string) (PriceResponse, error) {
	if len(offerIDs) == 0 || len(offerIDs) > 1000 {
		return PriceResponse{}, fmt.Errorf("список offerIDs пуст или содержит больше 1000 элементов")
	}
	type Filter struct {
		OfferID    []string `json:"offer_id"`
		ProductID  []string `json:"product_id"`
		Visibility string   `json:"visibility"`
	}
	type Request struct {
		Cursor *string `json:"cursor"`
		Filter Filter  `json:"filter"`
		Limit  int     `json:"limit"`
	}

	jsonData, err := json.Marshal(Request{
		Cursor: nil,
		Filter: Filter{
			OfferID:    offerIDs,
			ProductID:  nil,
			Visibility: "ALL",
		},
		Limit: 1000,
	})
	if err != nil {
		return PriceResponse{}, fmt.Errorf("ошибка сериализации данных: %v", err)
	}
	req, err := api.RequestWithAuthHeaders(
		"POST",
		"https://api-seller.ozon.ru/v5/product/info/prices",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return PriceResponse{}, fmt.Errorf("ошибка создания запроса: %v", err)
	}
	resp, err := api.session.Do(req)
	if err != nil {
		return PriceResponse{}, fmt.Errorf("ошибка отправки запроса: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return PriceResponse{}, fmt.Errorf("API error: %d %s", resp.StatusCode, string(body))
	}

	var response PriceResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return PriceResponse{}, fmt.Errorf("ошибка декодирования ответа: %v", err)
	}
	return response, nil
}

func (api *Api) ChangePrice(newPrices map[string]string) error {
	// 1. Валидация входных данных
	if len(newPrices) == 0 || len(newPrices) > 1000 {
		return fmt.Errorf("списки offerIDs и newPrices не могут быть пустыми")
	}

	// 2. Подготовка структуры запроса
	type PriceItem struct {
		OfferID string `json:"offer_id"`
		Price   string `json:"price"`
	}

	type PriceUpdateRequest struct {
		Prices []PriceItem `json:"prices"`
	}

	var priceItems []PriceItem
	for k, v := range newPrices {
		priceItems = append(priceItems, PriceItem{
			OfferID: k,
			Price:   v,
		})
	}

	// 3. Формирование JSON
	jsonData, err := json.Marshal(PriceUpdateRequest{Prices: priceItems})
	if err != nil {
		return fmt.Errorf("ошибка сериализации данных: %v", err)
	}

	// 4. Создание и настройка запроса
	req, err := api.RequestWithAuthHeaders(
		"POST",
		"https://api-seller.ozon.ru/v1/product/import/prices",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return fmt.Errorf("ошибка создания запроса: %v", err)
	}

	// 5. Отправка запроса
	resp, err := api.session.Do(req)
	if err != nil {
		return fmt.Errorf("ошибка отправки запроса: %v", err)
	}
	defer resp.Body.Close()

	// 6. Обработка ответа
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %d %s", resp.StatusCode, string(body))
	}

	var response *PriceChangeResponse
	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return fmt.Errorf("ошибка декодирования ответа: %v", err)
	}
	successfullyUpdated := 0
	for _, res := range response.Result {
		if !res.Updated {
			log.Printf("ошибка при обновлнеии цены %", res)
		} else {
			successfullyUpdated++
		}
	}

	log.Printf("Успешно обновлено %d цен", successfullyUpdated)
	return nil
}

func (api *Api) RequestWithAuthHeaders(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Id", api.arguments[params.CLIENT_ID])
	req.Header.Set("Api-Key", api.arguments[params.API_KEY])
	return req, nil
}
