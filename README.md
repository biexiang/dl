## Go下载客户端 
+ 使用 

        ./dlclient -l=http://dl.net/download/fs.mkv -c=200

        2018/10/27 16:34:59 Downloading:  http://dl.net/download/fs.mkv
        2018/10/27 16:35:02 Download Succeed  2.755458159s

+ 支持指定开启并行协程数进行下载
+ 支持断点续传、分块下载
+ Nginx 限速测试

        server {
            listen 80; 
            server_name dl.net;

            location /download {
                limit_rate_after 1m;
                limit_rate 500k;
                alias        /Users/poly/Downloads/Cut/;
                index        index.html;
            }

        }