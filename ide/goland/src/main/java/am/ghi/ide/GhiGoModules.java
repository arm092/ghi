package am.ghi.ide;

import com.google.gson.*;
import com.goide.vgo.VgoUtil;
import com.goide.sdk.GoSdkUtil;
import com.intellij.openapi.module.ModuleUtilCore;
import com.intellij.openapi.vfs.*;
import com.intellij.psi.PsiFile;
import java.nio.file.Path;
import java.util.*;

/** Read-only lookup of the exact module version recorded by Mojave. */
final class GhiGoModules {
    record Resolution(boolean pinned,VirtualFile directory) {}
    static Resolution resolve(PsiFile context,String importPath){
        if(!safePath(importPath))return new Resolution(true,null);
        VirtualFile lock=findLock(context);if(lock==null)return new Resolution(false,null);
        try{
            JsonObject root=JsonParser.parseString(VfsUtilCore.loadText(lock)).getAsJsonObject();
            if(!root.has("go")||!root.getAsJsonObject("go").has("modules"))return new Resolution(importPath.split("/",2)[0].contains("."),null);
            String selected="",version="";
            for(JsonElement value:root.getAsJsonObject("go").getAsJsonArray("modules")){
                JsonObject module=value.getAsJsonObject();String name=module.get("path").getAsString();
                if((importPath.equals(name)||importPath.startsWith(name+"/"))&&name.length()>selected.length()){
                    selected=name;version=module.get("version").getAsString();
                }
            }
            if(selected.isEmpty()||!safePath(selected)||!version.matches("v[0-9][A-Za-z0-9.+!_-]*"))return new Resolution(importPath.split("/",2)[0].contains("."),null);
            String relative=escape(selected)+"@"+escape(version)+importPath.substring(selected.length());
            var module=ModuleUtilCore.findModuleForPsiElement(context);Set<VirtualFile> caches=new LinkedHashSet<>();
            var configured=VgoUtil.getDependenciesRoot(context.getProject(),module);if(configured!=null)caches.add(configured);
            String environment=System.getenv("GOMODCACHE");
            if(environment!=null&&!environment.isBlank()){
                var cache=LocalFileSystem.getInstance().findFileByPath(environment.replace('\\','/'));if(cache!=null)caches.add(cache);
            }
            if(caches.isEmpty())for(var goPath:GoSdkUtil.getGoPathRoots(context.getProject(),module)){
                var cache=goPath.findFileByRelativePath("pkg/mod");if(cache!=null)caches.add(cache);
            }
            for(var cache:caches){var directory=cache.findFileByRelativePath(relative);if(directory!=null&&directory.isDirectory())return new Resolution(true,directory);}
        }catch(java.io.IOException|JsonParseException|IllegalStateException|IllegalArgumentException|NullPointerException ignored){
            // A lockfile can be incomplete while Mojave writes it. Never guess a version.
        }
        return new Resolution(importPath.split("/",2)[0].contains("."),null);
    }
    static List<String> importPaths(PsiFile context,String prefix){
        if(!prefix.startsWith("go:"))return List.of();VirtualFile lock=findLock(context);if(lock==null)return List.of();
        List<String> candidates=new ArrayList<>();String requested=prefix.substring(3);
        try{
            JsonObject root=JsonParser.parseString(VfsUtilCore.loadText(lock)).getAsJsonObject();
            if(!root.has("go")||!root.getAsJsonObject("go").has("modules"))return List.of();
            for(JsonElement value:root.getAsJsonObject("go").getAsJsonArray("modules")){
                String name=value.getAsJsonObject().get("path").getAsString();
                if(name.startsWith(requested)&&resolve(context,name).directory()!=null)candidates.add("go:"+name);
                if(requested.startsWith(name+"/")){
                    String parent=requested.substring(0,requested.lastIndexOf('/'));
                    VirtualFile directory=resolve(context,parent).directory();if(directory==null)continue;
                    for(var child:directory.getChildren())if(child.isDirectory()&&!child.getName().startsWith("_")&&!child.getName().startsWith(".")&&!Set.of("internal","vendor","testdata","cmd").contains(child.getName())){
                        String path="go:"+parent+"/"+child.getName();if(path.startsWith(prefix))candidates.add(path);
                    }
                }
            }
        }catch(java.io.IOException|JsonParseException|IllegalStateException|NullPointerException ignored){return List.of();}
        return candidates;
    }
    private static VirtualFile findLock(PsiFile context){
        var file=context.getOriginalFile().getVirtualFile();
        for(var directory=file==null?null:file.getParent();directory!=null;directory=directory.getParent()){
            var lock=directory.findChild("mojave.lock");if(lock!=null&&!lock.isDirectory())return lock;
            if(directory.getPath().equals(context.getProject().getBasePath()))break;
        }
        try{
            var settings=GhiSettings.get(context.getProject()).getState();
            Path root=GhiCommand.directory(context.getProject().getBasePath(),settings.directory);
            return LocalFileSystem.getInstance().findFileByNioFile(root.resolve("mojave.lock"));
        }catch(RuntimeException ignored){return null;}
    }
    private static boolean safePath(String path){return !path.isBlank()&&!path.contains("\\")&&!path.startsWith("/")&&Arrays.stream(path.split("/",-1)).noneMatch(part->part.isEmpty()||part.equals(".")||part.equals(".."));}
    private static String escape(String value){StringBuilder escaped=new StringBuilder();for(char ch:value.toCharArray()){if(ch>='A'&&ch<='Z')escaped.append('!').append(Character.toLowerCase(ch));else escaped.append(ch);}return escaped.toString();}
}
