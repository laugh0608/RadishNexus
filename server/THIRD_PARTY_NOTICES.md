# 新增 Markdown 依赖声明

本文件保留正式 Markdown parser 的上游许可。核心代码仍遵循仓库根许可证；其它 Go 依赖的许可证仍由各自 module 提供，本文不是所有依赖的完整清单。

## goldmark v1.8.6

- 模块：`github.com/yuin/goldmark v1.8.6`。
- 来源：[固定版本](https://github.com/yuin/goldmark/tree/v1.8.6)。
- 用途：CommonMark AST 解析及文本转义规则；不使用 HTML renderer 输出作为浏览器内容。
- Go 1.22，无第三方 require；正式版本与 checksum 由 `go.mod` / `go.sum` 固定，`check-server.sh` 验证 module 完整性。
- 以下许可取自精确版本 module 包 `LICENSE`；分发包含此依赖的工件时须保留。

```text
MIT License

Copyright (c) 2019 Yusuke Inuzuka

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
