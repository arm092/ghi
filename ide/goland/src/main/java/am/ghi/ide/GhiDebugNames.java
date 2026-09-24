package am.ghi.ide;

import com.google.gson.*;
import java.io.IOException;
import java.nio.file.*;
import java.util.*;

/** Compiler-authored names for the generated Go values visible through DWARF. */
final class GhiDebugNames {
    record Function(String name,boolean helper) {}
    final Map<String,String> fields;
    final Map<String,String> types;
    final Map<String,Function> functions;

    private GhiDebugNames(Map<String,String> fields,Map<String,String> types,Map<String,Function> functions){
        this.fields=fields;this.types=types;this.functions=functions;
    }
    static GhiDebugNames read(Path executable) throws IOException {
        Path path=Path.of(executable.toString()+".ghi-debug.json");
        JsonObject document;
        try{document=JsonParser.parseString(Files.readString(path)).getAsJsonObject();}
        catch(JsonParseException|IllegalStateException error){throw new IOException("Invalid Ghi debug names: "+path,error);}
        try{
            if(document.get("version").getAsInt()!=1)throw new IOException("Unsupported Ghi debug names: "+path);
            Map<String,String> fields=strings(document.getAsJsonObject("fields"));
            Map<String,String> types=strings(document.getAsJsonObject("types"));
            Map<String,Function> functions=new HashMap<>();
            for(var item:document.getAsJsonObject("functions").entrySet()){
                JsonObject function=item.getValue().getAsJsonObject();
                functions.put(item.getKey(),new Function(function.get("name").getAsString(),function.get("helper").getAsBoolean()));
            }
            return new GhiDebugNames(fields,types,functions);
        }catch(IllegalStateException|IllegalArgumentException|NullPointerException error){throw new IOException("Invalid Ghi debug names: "+path,error);}
    }
    private static Map<String,String> strings(JsonObject object){
        Map<String,String> result=new HashMap<>();
        for(var item:object.entrySet())result.put(item.getKey(),item.getValue().getAsString());
        return result;
    }
    Function function(String name){
        Function exact=functions.get(name);if(exact!=null)return exact;
        Function generic=functions.get(withoutTypeArguments(name));
        return generic==null?null:new Function(generic.name()+trailingTypeArguments(name),generic.helper());
    }
    String field(String name){return fields.getOrDefault(name,name);}
    boolean hasType(String name){return types.containsKey(name)||types.containsKey(withoutTypeArguments(name));}
    String type(String name){
        String exact=types.get(name);if(exact!=null)return exact;
        String generic=types.get(withoutTypeArguments(name));
        return generic==null?name:generic+trailingTypeArguments(name);
    }
    private static String withoutTypeArguments(String symbol){
        var result=new StringBuilder();int depth=0;
        for(int index=0;index<symbol.length();index++){
            char ch=symbol.charAt(index);
            if(ch=='['){depth++;continue;}
            if(ch==']'&&depth>0){depth--;continue;}
            if(depth==0)result.append(ch);
        }
        return depth==0?result.toString():symbol;
    }
    private static String trailingTypeArguments(String symbol){
        if(!symbol.endsWith("]"))return "";
        int depth=0;
        for(int index=symbol.length()-1;index>=0;index--){
            char ch=symbol.charAt(index);
            if(ch==']')depth++;
            else if(ch=='['&&--depth==0)return symbol.substring(index).replace("go.shape.","");
        }
        return "";
    }
}
