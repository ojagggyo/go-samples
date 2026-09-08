package main

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestLoginForms(t *testing.T) {
	for _, stepped := range []bool{false, true} {
		name := "single"
		if stepped {
			name = "stepped"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := chromedp.NewContext(context.Background())
			defer cancel()
			ctx, timeout := context.WithTimeout(ctx, 60*time.Second)
			defer timeout()
			password := `<input type="password" onchange="window.passwordInput=this.value"><button onclick="window.done=window.userInput==='test-user' && window.passwordInput==='test-pass'">ログイン</button>`
			next := password
			if stepped {
				next = `<button type="button" onclick="setTimeout(() => { document.querySelector('#stage').innerHTML = document.querySelector('template').innerHTML }, 300)">次へ</button><template>` + password + `</template>`
			}
			page := `<input type="text" style="display:none"><input type="text" readonly value="ignore"><div id="form"></div><script>setTimeout(() => {
			 document.querySelector('#form').innerHTML = document.querySelector('#initial').innerHTML;
			}, 300)</script><template id="initial"><input id="username" name="username" value="previous-user" onchange="window.userInput=this.value"><div id="stage">` + next + `</div></template>`
			// The live ID input omits its type attribute, so input[type='text']
			// cannot match it. Exercise the shipped configuration against that DOM.
			cfg, err := loadConfig("config.example.json")
			if err != nil {
				t.Fatal(err)
			}
			if err := chromedp.Run(ctx, chromedp.Navigate("data:text/html;charset=utf-8,"+url.PathEscape(page))); err != nil {
				t.Fatal(err)
			}
			if err := login(ctx, cfg, "test-user", "test-pass"); err != nil {
				t.Fatal(err)
			}
			var done bool
			if err := chromedp.Run(ctx, chromedp.Evaluate("window.done === true", &done)); err != nil {
				t.Fatal(err)
			}
			if !done {
				t.Fatal("login did not submit the entered credentials")
			}
		})
	}
}
