package ozon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"time"
)

type ReviewText struct {
	Positive string `json:"positive"`
	Negative string `json:"negative"`
	Comment  string `json:"comment"`
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

type ReviewsList struct {
	Result                  []Review `json:"result"`
	HasNext                 bool     `json:"hasNext"`
	PaginationLastTimestamp string   `json:"pagination_last_timestamp"` // Изменено на string
	PaginationLastUUID      string   `json:"pagination_last_uuid"`
}

type params struct {
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

type Api struct {
	sleep             time.Duration
	session           *http.Client
	url               string
	answerURL         string
	commentAnswersURL string
	companyID         string
	loadMoreParams    params
	headers           map[string]string
}

func NewApi() *Api {
	companyId, exists := os.LookupEnv("COMPANY_ID")
	if !exists {
		log.Fatal("задайте COOKIES_PATH")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Fatalf("Got error while creating cookie jar %s", err.Error())
	}
	api := &Api{
		sleep: 2, // TODO parametrize
		session: &http.Client{
			Jar: jar,
		},
		url:               "https://seller.ozon.ru/api/v3/review/list",
		answerURL:         "https://seller.ozon.ru/api/review/comment/create",
		commentAnswersURL: "https://seller.ozon.ru/api/review/comment/list",
		companyID:         companyId,
		loadMoreParams: params{
			WithCounters:            false,
			Sort:                    map[string]string{"sort_by": "PUBLISHED_AT", "sort_direction": "DESC"},
			CompanyType:             "seller",
			Filter:                  map[string]interface{}{"interaction_status": []string{"NOT_VIEWED", "VIEWED"}},
			CompanyId:               companyId,
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
			"x-o3-company-id": companyId,
			"x-o3-language":   "ru",
			"x-o3-page-type":  "review",
		},
	}
	loadCookies(api)
	return api
}

func loadCookies(api *Api) {
	cookiesPath, exists := os.LookupEnv("COOKIES_PATH")
	if !exists {
		log.Fatal("задайте COOKIES_PATH")
	}
	plan, _ := os.ReadFile(cookiesPath)
	var data []cookie
	err := json.Unmarshal(plan, &data)
	if err != nil {
		log.Fatal(err)
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

func GetChunk(api *Api) ReviewsList {
	params, err := json.Marshal(api.loadMoreParams)
	if err != nil {
		log.Fatal(err)
	}
	req, err := http.NewRequest("POST", "https://seller.ozon.ru/api/v3/review/list", bytes.NewBuffer(params))
	for k, v := range api.headers {
		req.Header.Set(k, v)
	}
	if err != nil {
		log.Fatalf("Got error %s", err.Error())
	}
	resp, err := api.session.Do(req)
	if err != nil {
		log.Fatalf("Error occured. Error is: %s", err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode > 299 {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatal(err)
		}
		bodyString := string(bodyBytes)
		fmt.Printf("StatusCode: %d %s", resp.StatusCode, bodyString)
	}
	var res ReviewsList
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		log.Fatalf("Ошибка декодирования %v", err)
	}
	api.loadMoreParams.PaginationLastUuid = &res.PaginationLastUUID
	api.loadMoreParams.PaginationLastTimestamp = &res.PaginationLastTimestamp
	return res
}
