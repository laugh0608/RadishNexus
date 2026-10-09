import groovy.json.JsonLexer
import groovy.json.JsonTokenType as T
import java.nio.charset.CodingErrorAction
import java.nio.charset.StandardCharsets

// The bundled JsonSlurper accepts duplicate keys. Persistent state must not.
class StrictJson {
    private JsonLexer lexer
    private def token
    static Object parse(byte[] bytes) {
        def decoder = StandardCharsets.UTF_8.newDecoder()
            .onMalformedInput(CodingErrorAction.REPORT).onUnmappableCharacter(CodingErrorAction.REPORT)
        def parser = new StrictJson(lexer: new JsonLexer(new StringReader(decoder.decode(java.nio.ByteBuffer.wrap(bytes)).toString())))
        parser.advance()
        def value = parser.value(0)
        if (parser.token != null) throw new IllegalArgumentException('invalid_json')
        value
    }
    private void advance() { token = lexer.nextToken() }
    private void expect(def type) {
        if (token?.type != type) throw new IllegalArgumentException('invalid_json')
        advance()
    }
    private Object value(int depth) {
        if (depth > 8 || token == null) throw new IllegalArgumentException('invalid_json')
        if (token.type == T.OPEN_CURLY) {
            advance()
            Map result = new LinkedHashMap()
            if (token?.type != T.CLOSE_CURLY) {
                while (true) {
                    if (token?.type != T.STRING) throw new IllegalArgumentException('invalid_json')
                    String key = token.value
                    if (result.containsKey(key)) throw new IllegalArgumentException('duplicate_key')
                    advance(); expect(T.COLON)
                    result[key] = value(depth + 1)
                    if (token?.type != T.COMMA) break
                    advance()
                }
            }
            expect(T.CLOSE_CURLY)
            return result
        }
        if (token.type in [T.STRING, T.NUMBER, T.TRUE, T.FALSE, T.NULL]) {
            def result = token.value
            advance()
            return result
        }
        // The collector formats contain objects and scalars only.
        throw new IllegalArgumentException('invalid_json')
    }
}
