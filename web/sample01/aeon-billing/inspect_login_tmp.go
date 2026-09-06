//go:build ignore

package main

import (
 "context"
 "fmt"
 "time"
 "github.com/chromedp/chromedp"
)

func main() {
 ctx, cancel := chromedp.NewContext(context.Background()); defer cancel()
 ctx, timeout := context.WithTimeout(ctx, 45*time.Second); defer timeout()
 var result string
 err := chromedp.Run(ctx, chromedp.Navigate("https://www.aeon.co.jp/app/"), chromedp.Sleep(15*time.Second), chromedp.Evaluate(`JSON.stringify({url:location.origin+location.pathname, title:document.title, denied:document.body.innerText.includes('Access Denied'), fields:Array.from(document.querySelectorAll('input')).map(e=>({tag:e.tagName,type:e.type,id:e.id,name:e.name,placeholder:e.placeholder,visible:e.getClientRects().length>0})),frames:Array.from(document.querySelectorAll('iframe')).map(e=>({src:e.src.split('?')[0],visible:e.getClientRects().length>0})),buttons:Array.from(document.querySelectorAll('button,input[type=submit]')).map(e=>({type:e.type,text:e.innerText,disabled:e.disabled}))})`, &result))
 fmt.Println(result); if err != nil { fmt.Println(err) }
}
