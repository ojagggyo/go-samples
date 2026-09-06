echo off
cd /d D:\GitHub\go-samples\kakao\sample01\kakao-send
set KAKAO_REST_API_KEY="6e1a96882e2e8cf03687f7fb737e7ccd"
set KAKAO_CLIENT_SECRET="Omul1nbK8EOvwJnnMwp4UtK2reK64mDq"

echo on
echo %KAKAO_REST_API_KEY%
echo %KAKAO_CLIENT_SECRET%

go run . -url "https://www.youtube.com/watch?v=JS5Ro31ua8Q" -message "おすすめ動画"