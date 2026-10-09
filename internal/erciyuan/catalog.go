package erciyuan

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (c *Client) Categories(ctx context.Context) ([]Category, error) {
	c.mu.Lock()
	if len(c.categories) > 0 && time.Now().Before(c.categoryUntil) {
		result := append([]Category(nil), c.categories...)
		c.mu.Unlock()
		return result, nil
	}
	c.mu.Unlock()
	data, err := c.api(ctx, "/filter/nav", nil)
	if err != nil {
		return nil, err
	}
	rows := objectRows(data["list"])
	categories := make([]Category, 0, len(rows))
	seen := map[int]bool{}
	for _, row := range rows {
		id := intValue(row["type_id"])
		name := strings.TrimSpace(stringValue(row["type_name"]))
		if id > 0 && name != "" && !seen[id] {
			categories = append(categories, Category{ID: id, Name: name})
			seen[id] = true
		}
	}
	if len(categories) == 0 {
		return nil, errors.New("二次元返回空分类列表")
	}
	c.mu.Lock()
	c.categories = append([]Category(nil), categories...)
	c.categoryUntil = time.Now().Add(time.Hour)
	c.mu.Unlock()
	return categories, nil
}

func clonePage(p Page) Page {
	clone := p
	clone.Items = make([]map[string]any, 0, len(p.Items))
	for _, row := range p.Items {
		clone.Items = append(clone.Items, cloneRow(row))
	}
	return clone
}

func (c *Client) categoryPage(ctx context.Context, category Category, page int) (Page, error) {
	if page == 1 {
		c.mu.Lock()
		cached, ok := c.firstPages[category.ID]
		c.mu.Unlock()
		if ok && time.Now().Before(cached.until) {
			result := clonePage(cached.page)
			c.remember(result.Items)
			return result, nil
		}
	}
	params := url.Values{"pg": {strconv.Itoa(page)}, "tid": {strconv.Itoa(category.ID)}, "class": {""}, "area": {""}, "lang": {""}, "year": {""}, "order": {"time"}}
	data, err := c.api(ctx, "/filter/qjvideo", params)
	if err != nil {
		return Page{}, err
	}
	if _, ok := data["list"].([]any); !ok {
		return Page{}, errors.New("二次元上游影片列表结构无效")
	}
	if intValue(data["page"]) != page {
		return Page{}, errors.New("二次元上游分页游标不一致")
	}
	result := Page{Page: page, Total: intValue(data["total"]), PageCount: intValue(data["pagecount"]), Items: objectRows(data["list"])}
	if result.Total < 0 || result.PageCount < 0 || result.PageCount > 100000 || (len(result.Items) > 0 && result.PageCount < page) {
		return Page{}, errors.New("二次元上游分页信息无效")
	}
	if result.PageCount == 0 && len(result.Items) == 0 {
		result.PageCount = 1
	}
	for _, row := range result.Items {
		row["type_id"] = category.ID
		row["type_name"] = category.Name
	}
	c.remember(result.Items)
	if page == 1 {
		c.mu.Lock()
		c.firstPages[category.ID] = cachedPage{page: clonePage(result), until: time.Now().Add(2 * time.Minute)}
		c.mu.Unlock()
	}
	return result, nil
}

// List uses filter/nav type IDs, never home/navigation IDs. The source has no
// documented global tid or hour filter: category 0 concatenates each category's
// actual page cursor. For recent jobs, take one time-sorted page per category;
// do not filter on vod_time_add (creation time is not an update timestamp).
func (c *Client) List(ctx context.Context, categoryID, page, hours int) (Page, error) {
	if categoryID < 0 || page < 1 || page > 100000 || hours < 0 {
		return Page{}, errors.New("二次元采集分页参数无效")
	}
	categories, err := c.Categories(ctx)
	if err != nil {
		return Page{}, err
	}
	if categoryID > 0 {
		for _, category := range categories {
			if category.ID != categoryID {
				continue
			}
			if hours > 0 && page > 1 {
				return Page{Page: page, PageCount: 1, Items: []map[string]any{}}, nil
			}
			result, err := c.categoryPage(ctx, category, page)
			if err == nil && hours > 0 {
				result.Total = len(result.Items)
				result.PageCount = 1
			}
			return result, err
		}
		return Page{}, errors.New("二次元分类不存在，请重新拉取资源站分类")
	}
	first := make([]Page, len(categories))
	total, pageCount := 0, 0
	for index, category := range categories {
		if ctx.Err() != nil {
			return Page{}, ctx.Err()
		}
		first[index], err = c.categoryPage(ctx, category, 1)
		if err != nil {
			return Page{}, err
		}
		if hours > 0 {
			total += len(first[index].Items)
			pageCount++
		} else {
			total += first[index].Total
			pageCount += first[index].PageCount
		}
	}
	if page > pageCount {
		return Page{Page: page, Total: total, PageCount: pageCount, Items: []map[string]any{}}, nil
	}
	cursor := page
	for index, category := range categories {
		count := first[index].PageCount
		if hours > 0 {
			count = 1
		}
		if cursor > count {
			cursor -= count
			continue
		}
		var result Page
		if cursor == 1 {
			result = clonePage(first[index])
		} else {
			result, err = c.categoryPage(ctx, category, cursor)
			if err != nil {
				return Page{}, err
			}
		}
		result.Total = total
		result.Page = page
		result.PageCount = pageCount
		return result, nil
	}
	return Page{}, errors.New("二次元分页游标无法定位")
}

func (c *Client) Search(ctx context.Context, keyword string) ([]map[string]any, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" || len([]rune(keyword)) > 100 || strings.ContainsAny(keyword, "\x00\r\n") {
		return nil, errors.New("二次元搜索关键词无效")
	}
	data, err := c.api(ctx, "/vod/search", url.Values{"wd": {keyword}})
	if err != nil {
		return nil, err
	}
	rows := objectRows(data["data"])
	// Some original responses encrypt the data field independently rather than
	// the entire JSON document. Decode with the same verified original material.
	if encrypted, ok := data["data"].(string); ok && encrypted != "" {
		decoded, err := decodeDocument([]byte(encrypted))
		if err != nil {
			return nil, err
		}
		rows = objectRows(decoded)
		if _, ok := decoded.([]any); !ok {
			return nil, errors.New("二次元上游搜索列表结构无效")
		}
	} else if _, ok := data["data"].([]any); !ok {
		return nil, errors.New("二次元上游搜索列表结构无效")
	}
	c.remember(rows)
	return rows, nil
}
