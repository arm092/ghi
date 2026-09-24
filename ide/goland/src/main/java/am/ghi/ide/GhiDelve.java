package am.ghi.ide;

import com.google.gson.*;
import java.io.*;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.concurrent.*;
import java.util.concurrent.atomic.AtomicInteger;

/** Delve's existing JSON-RPC 2 service, with one reader for concurrent commands. */
final class GhiDelve implements AutoCloseable {
    private final Socket socket;
    private final BufferedWriter output;
    private final Map<Integer,CompletableFuture<JsonObject>> pending=new ConcurrentHashMap<>();
    private final AtomicInteger sequence=new AtomicInteger();
    private final Thread reader;

    GhiDelve(InetSocketAddress address) throws IOException {
        socket=new Socket();socket.connect(address,10000);socket.setTcpNoDelay(true);
        output=new BufferedWriter(new OutputStreamWriter(socket.getOutputStream(),StandardCharsets.UTF_8));
        reader=new Thread(this::read,"Ghi Delve replies");reader.setDaemon(true);reader.start();
    }

    CompletableFuture<JsonObject> request(String operation,JsonObject arguments){
        int id=sequence.incrementAndGet();
        var answer=new CompletableFuture<JsonObject>();pending.put(id,answer);
        JsonObject request=new JsonObject();request.addProperty("id",id);request.addProperty("method","RPCServer."+operation);
        JsonArray params=new JsonArray();params.add(arguments);request.add("params",params);
        try{ synchronized(output){output.write(request.toString());output.write('\n');output.flush();} }
        catch(IOException error){pending.remove(id);answer.completeExceptionally(error);}
        return answer;
    }

    private void read(){
        try(var input=new BufferedReader(new InputStreamReader(socket.getInputStream(),StandardCharsets.UTF_8))){
            String line;
            while((line=input.readLine())!=null){
                JsonObject reply=JsonParser.parseString(line).getAsJsonObject();
                if(!reply.has("id"))continue;
                var future=pending.remove(reply.get("id").getAsInt());if(future==null)continue;
                JsonElement error=reply.get("error");
                if(error!=null&&!error.isJsonNull())future.completeExceptionally(new IOException(error.isJsonPrimitive()?error.getAsString():error.toString()));
                else future.complete(reply.has("result")&&reply.get("result").isJsonObject()?reply.getAsJsonObject("result"):new JsonObject());
            }
        }catch(IOException|JsonParseException|IllegalStateException error){
            fail(error);
        }finally{fail(new EOFException("Delve connection closed"));}
    }

    private void fail(Exception error){for(var answer:pending.values())answer.completeExceptionally(error);pending.clear();}
    @Override public void close() throws IOException {socket.close();}
}
