# tempMail
极简临时邮箱，无数据库，阅后即焚，0点清空，支持多域名

# 配置方法
.env配置域名，支持多个域名

域名自行解析mx到服务器

主域名自行解析A记录到服务器

如果需要https,env自行配置证书路径

# 使用方法
http://hostIp/getAllowedDomains

获取所有域名后缀

http://hostIp/xxx@xx.xx

直接请求邮箱获取邮件，阅后即焚

当然你也可以搞个域名A记录到你的hostIp

