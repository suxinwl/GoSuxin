package suxinvideo

const articleThemeBody = `<div class="vdesc sx-article"><h1>{{.Article.title}}</h1><p class="sx-article-time">{{.Article.addtime}}</p><div class="sx-article-content">{{.Article.content}}</div></div>`
const articlesThemeBody = `<div class="sec-h"><h3>网站公告</h3></div>{{range .Articles}}<a class="vdesc sx-article-link" href="/suxinvideo/article?id={{.id}}"><b>{{.title}}</b><span>{{.addtime}}</span></a>{{else}}<p class="sx-empty">暂无网站公告</p>{{end}}`
